// Package status decides whether the required checks are satisfied for the current inputs.
package status

import (
	"fmt"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
)

// State of one check relative to the current inputs.
type State string

const (
	Pass    State = "pass"    // latest receipt passed and matches current inputs
	Fail    State = "fail"    // latest receipt matches current inputs but did not pass
	Stale   State = "stale"   // inputs or command changed since the latest receipt
	Missing State = "missing" // never run (not-run is never a pass)
	Empty   State = "empty"   // inputs match no files, so a receipt would prove nothing
)

// CheckStatus is one row of a status report.
type CheckStatus struct {
	Check       string           `json:"check"`
	State       State            `json:"state"`
	CurrentHash string           `json:"current_hash"`
	Receipt     *receipt.Receipt `json:"receipt,omitempty"`
}

// Report is the status of every check a mode requires.
type Report struct {
	Mode      string        `json:"mode"`
	Satisfied bool          `json:"satisfied"`
	Checks    []CheckStatus `json:"checks"`
}

// Evaluate computes the report for mode without running anything.
func Evaluate(root string, cfg *config.Config, mode string) (Report, error) {
	names, err := cfg.ModeChecks(mode)
	if err != nil {
		return Report{}, err
	}
	store := receipt.NewStore(root)
	rep := Report{Mode: mode, Satisfied: true}
	for _, name := range names {
		ch := cfg.Checks[name]
		d, err := fileset.Compute(root, ch.Inputs, cfg.Exclude, cfg.Fingerprint(name))
		if err != nil {
			return Report{}, fmt.Errorf("check %q: %w", name, err)
		}
		r, err := store.Get(name)
		if err != nil {
			return Report{}, err
		}
		cs := CheckStatus{Check: name, CurrentHash: d.Hash, Receipt: r}
		switch {
		case d.FileCount == 0:
			cs.State = Empty
		case r == nil:
			cs.State = Missing
		case r.InputsHash != d.Hash:
			cs.State = Stale
		case r.Result != receipt.ResultPass:
			cs.State = Fail
		default:
			cs.State = Pass
		}
		if cs.State != Pass {
			rep.Satisfied = false
		}
		rep.Checks = append(rep.Checks, cs)
	}
	return rep, nil
}
