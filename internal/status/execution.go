package status

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/environment"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

const (
	NotRun       State = "not-run"
	Running      State = "running"
	Interrupted  State = "interrupted"
	Cancelled    State = "cancelled"
	Invalid      State = "invalid"
	Incompatible State = "incompatible"
	InputChanged State = "input-changed"
	InputError   State = "input-error"
)

// UnverifiedState is independent of whether a selected mode's checks pass.
type UnverifiedState struct {
	Present bool    `json:"present"`
	Reason  *string `json:"reason"`
}

func unverifiedState(root string) UnverifiedState {
	path := filepath.Join(root, ".naru/state/unverified.json")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return UnverifiedState{}
	}
	reason := "not_verified"
	data, err := os.ReadFile(path) // #nosec G304 -- Fixed marker under the selected local project root.
	var marker struct {
		Reason string `json:"reason"`
	}
	if err != nil {
		reason = "marker_unreadable"
	} else if json.Unmarshal(data, &marker) != nil || marker.Reason == "" {
		reason = "invalid_marker"
	}
	return UnverifiedState{Present: true, Reason: &reason}
}

// EvaluateState holds each check's lock while reading its state and inputs.
// Busy checks are never inferred to pass from an older record.
func EvaluateState(root string, cfg *config.Config, mode string) (Report, error) {
	names, err := cfg.ModeChecks(mode)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Schema: 2, Mode: mode, Satisfied: true, Unverified: unverifiedState(root)}
	for _, name := range names {
		row, err := evaluateCheck(root, cfg, name)
		if err != nil {
			return Report{}, fmt.Errorf("check %q: %w", name, err)
		}
		if row.State != Pass {
			rep.Satisfied = false
		}
		rep.Checks = append(rep.Checks, row)
	}
	return rep, nil
}
func evaluateCheck(root string, cfg *config.Config, name string) (row CheckStatus, retErr error) {
	row.Check = name
	set := func(state State, reason, message string) (CheckStatus, error) {
		row.State = state
		row.Reason = reason
		row.Message = message
		return row, nil
	}
	lock, err := runlock.Acquire(root, name)
	if errors.Is(err, runlock.ErrBusy) {
		return set(Running, "execution_active", "check is already running")
	}
	if err != nil {
		return row, err
	}
	defer func() { retErr = errors.Join(retErr, lock.Close()) }()
	root = lock.Root
	e, err := receipt.NewStateStore(root).Read(name)
	if errors.Is(err, receipt.ErrUnsupported) {
		return set(Incompatible, "unsupported_contract", "receipt schema or policy is unsupported")
	}
	if errors.Is(err, receipt.ErrInvalid) {
		return set(Invalid, "invalid_receipt", "receipt is corrupt or inconsistent; preserve it before recovery")
	}
	if err != nil {
		return row, err
	}
	row.Record = e.Record
	row.Receipt = e.Legacy
	if e.Relay != nil {
		return evaluateRelay(root, cfg, name, *e.Relay, time.Now().UTC())
	}
	if r := e.Record; r != nil {
		row.RunID = &r.RunID
		row.Provenance = &r.Provenance
		if r.Phase == receipt.PhaseRunning {
			return set(Interrupted, "owner_lost", "execution owner exited without a terminal record; manual recovery required")
		}
	}
	ch := cfg.Checks[name]
	if r := e.Record; r != nil && r.Execution.Result != nil && *r.Execution.Result == receipt.ResultNotRun {
		return set(NotRun, r.Execution.Unavailable.Reason, r.Execution.Unavailable.Message())
	}
	if unavailable := environment.Inspect(ch.Requires); unavailable != nil {
		return set(NotRun, unavailable.Reason, unavailable.Message())
	}
	d, err := fileset.Compute(root, ch.Inputs, cfg.Exclude, cfg.Fingerprint(name))
	if err != nil {
		return row, err
	}
	row.CurrentHash = d.Hash
	if d.FileCount == 0 {
		return set(Empty, "empty_inputs", "inputs match no files")
	}
	if e.Record == nil && e.Legacy == nil {
		return set(Missing, "not_run", "never run")
	}
	if r := e.Legacy; r != nil {
		row.Provenance = &receipt.Provenance{Kind: r.Evidence}
		if r.InputsHash != d.Hash {
			return set(Stale, "inputs_changed", "inputs or check definition changed since the last run")
		}
		if r.Result != receipt.ResultPass {
			return set(Fail, "command_failed", fmt.Sprintf("%s (exit %d)", r.Result, r.ExitCode))
		}
		return set(Incompatible, "policy_mismatch", "legacy receipt has no after-input evidence; rerun the check")
	}
	r := e.Record
	switch r.Inputs.Validity {
	case receipt.InputsChanged:
		return set(InputChanged, *r.Inputs.Reason, "inputs changed while the check ran; rerun the check")
	case receipt.InputsUnreadable:
		return set(InputError, *r.Inputs.Reason, "execution inputs could not be verified")
	}
	if r.Inputs.After.Hash != d.Hash || r.Inputs.After.FileCount != d.FileCount {
		return set(Stale, "inputs_changed", "inputs or check definition changed since the last run")
	}
	switch *r.Execution.Result {
	case receipt.ResultPass:
		return set(Pass, "verified", "check passed and before/after/current inputs match")
	case receipt.ResultCancelled:
		return set(Cancelled, "cancelled", "check was cancelled; rerun the check")
	case receipt.ResultInterrupted:
		return set(Interrupted, "owner_lost", "execution was interrupted; inspect surviving processes")
	default:
		code := "unknown"
		if r.Execution.ExitCode != nil {
			code = fmt.Sprint(*r.Execution.ExitCode)
		}
		return set(Fail, "command_failed", fmt.Sprintf("%s (exit %s) running `%s`, log %s", *r.Execution.Result, code, r.Execution.Command, r.Execution.LogPath))
	}
}
