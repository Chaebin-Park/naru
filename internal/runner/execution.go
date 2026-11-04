package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/environment"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

// ErrRecovery means an earlier owner disappeared without a terminal record.
// A free OS lock does not prove that its child processes have exited.
var ErrRecovery = errors.New("interrupted execution needs manual recovery")

type executionStore interface {
	Current(string) (*receipt.Record, error)
	Begin(receipt.Record) error
	Put(receipt.Record) error
}

func runState(root string, cfg *config.Config, name string, opt Options, openLog func(string) (logSink, error)) (outcome Outcome, retErr error) {
	if err := cfg.Validate(); err != nil {
		return Outcome{}, err
	}
	if _, ok := cfg.Checks[name]; !ok {
		return Outcome{}, fmt.Errorf("unknown check %q", name)
	}
	if opt.Evidence == "" {
		opt.Evidence = receipt.EvidenceLocal
	}
	if opt.Version == "" {
		opt.Version = "dev"
	}
	switch opt.Evidence {
	case receipt.EvidenceLocal, receipt.EvidenceAgent, receipt.EvidenceCI:
	default:
		return Outcome{}, errors.New("invalid evidence source")
	}
	parent := opt.Context
	if parent == nil {
		parent = context.Background()
	}
	if parent.Err() != nil {
		return Outcome{}, ErrInterrupted
	}
	lock, err := runlock.Acquire(root, name)
	if err != nil {
		return Outcome{}, err
	}
	defer func() { retErr = errors.Join(retErr, lock.Close()) }()
	root = lock.Root
	if parent.Err() != nil {
		return Outcome{}, ErrInterrupted
	}
	var store executionStore = receipt.NewStateStore(root)
	if opt.store != nil {
		store = opt.store(root)
	}
	old, err := store.Current(name)
	if err != nil {
		return Outcome{}, fmt.Errorf("read current execution: %w", err)
	}
	if old != nil && (old.Phase == receipt.PhaseRunning || (old.Inputs.Reason != nil && *old.Inputs.Reason == "owner_lost")) {
		return Outcome{}, fmt.Errorf("%w: %s run %s; inspect and stop surviving check processes, then preserve and move its current state before retrying", ErrRecovery, name, old.RunID)
	}
	// Read after acquiring the lock so a delayed request does not use stale config.
	cfg, err = config.Load(root)
	if err != nil {
		return Outcome{}, err
	}
	ch, ok := cfg.Checks[name]
	if !ok {
		return Outcome{}, fmt.Errorf("check %q was removed", name)
	}
	unavailable := environment.Inspect(ch.Requires)
	var before fileset.Digest
	if unavailable == nil {
		before, err = fileset.Compute(root, ch.Inputs, cfg.Exclude, cfg.Fingerprint(name))
		if err != nil {
			return Outcome{}, err
		}
		if before.FileCount == 0 {
			return Outcome{}, ErrEmptyInputs
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Outcome{}, err
	}
	id := hex.EncodeToString(random[:])
	logRel := ".naru/logs/" + name + "-" + id + ".log"
	logPath := filepath.Join(root, filepath.FromSlash(logRel))
	dir := ch.Workdir
	if dir == "" {
		dir = "."
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	started := now().UTC()
	elapsedStart := time.Now()
	r := receipt.Record{Schema: receipt.StateSchema, Check: name, RunID: id, Phase: receipt.PhaseRunning, StartedAt: started,
		Provenance: receipt.Provenance{Kind: opt.Evidence}, Execution: receipt.Execution{Command: ch.Run, Workdir: dir, TimeoutMS: ch.TimeoutDuration().Milliseconds(), LogPath: logRel, NaruVersion: opt.Version},
		Inputs: receipt.Inputs{Policy: receipt.InputPolicy, Before: inputDigest(before), Validity: receipt.InputsPending}}
	if parent.Err() != nil {
		return Outcome{}, ErrInterrupted
	}
	// Persist denial before log I/O can fail and leave an earlier pass authoritative.
	if unavailable != nil {
		result, duration := receipt.ResultNotRun, int64(0)
		r.Phase = receipt.PhaseTerminal
		r.FinishedAt = &started
		r.Execution.Result = &result
		r.Execution.DurationMS = &duration
		r.Execution.Unavailable = unavailable
		r.Inputs.Validity = receipt.InputsUnobserved
		if err := store.Begin(r); err != nil {
			return Outcome{}, fmt.Errorf("save unavailable execution: %w", err)
		}
	}
	if info, err := os.Lstat(filepath.Dir(logPath)); err == nil && !info.IsDir() {
		return Outcome{}, errors.New("log directory is not a plain directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Outcome{}, err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return Outcome{}, err
	}
	logFile, err := openLog(logPath)
	if err != nil {
		return Outcome{}, err
	}
	closed := false
	defer func() {
		if !closed {
			retErr = errors.Join(retErr, logFile.Close())
		}
	}()
	if _, err := fmt.Fprintf(logFile, "naru: check %q, run %s\n$ %s\n(dir: %s, inputs: %d files)\n\n", name, id, ch.Run, dir, before.FileCount); err != nil {
		return Outcome{}, err
	}
	if unavailable != nil {
		_, writeErr := fmt.Fprintln(logFile, "naru: not-run: "+unavailable.Message())
		syncErr := logFile.Sync()
		closed = true
		closeErr := logFile.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return Outcome{}, err
		}
		return Outcome{Receipt: legacyView(r), Record: &r}, nil
	}
	if parent.Err() != nil {
		return Outcome{}, ErrInterrupted
	}
	if err := store.Begin(r); err != nil {
		return Outcome{}, fmt.Errorf("save execution start: %w", err)
	}
	var tail tailBuffer
	writers := []io.Writer{logFile, &tail}
	if opt.Stream != nil {
		writers = append(writers, opt.Stream)
	}
	out := &checkedOutput{Writer: io.MultiWriter(writers...)}
	ctx, cancel := context.WithTimeout(parent, ch.TimeoutDuration())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", ch.Run) // #nosec G204 -- Trusted project checks explicitly execute configured shell commands.
	cmd.Dir = filepath.Join(root, filepath.FromSlash(dir))
	cmd.Env = append(os.Environ(), "NARU_CHECK="+name, "NARU_ROOT="+root, "NARU_RUN_ID="+id)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = waitDelay
	setProcessGroup(cmd)
	result, exitCode := receipt.ResultPass, 0
	runErr := cmd.Run()
	switch {
	case parent.Err() != nil:
		result, exitCode = receipt.ResultCancelled, -1
		_, _ = fmt.Fprintf(out, "\nnaru: check %q interrupted\n", name)
	case ctx.Err() == context.DeadlineExceeded:
		result, exitCode = receipt.ResultTimeout, -1
		_, _ = fmt.Fprintf(out, "\nnaru: check %q timed out after %s\n", name, ch.TimeoutDuration())
	case errors.Is(runErr, exec.ErrWaitDelay):
		exitCode = cmd.ProcessState.ExitCode()
		if exitCode != 0 {
			result = receipt.ResultFail
		}
		_, _ = fmt.Fprintf(out, "\nnaru: check %q exited %d but a background process kept its output open; it was not waited for\n", name, exitCode)
	case runErr != nil:
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			result, exitCode = receipt.ResultFail, ee.ExitCode()
		} else {
			result, exitCode = receipt.ResultError, -1
			_, _ = fmt.Fprintf(out, "\nnaru: could not complete check %q: %v\n", name, runErr)
		}
	}
	end := now().UTC()
	if end.Before(started) {
		end = started
	}
	duration := time.Since(elapsedStart).Milliseconds()
	r.Phase = receipt.PhaseTerminal
	r.FinishedAt = &end
	r.Execution.Result = &result
	r.Execution.ExitCode = &exitCode
	r.Execution.DurationMS = &duration
	r.Inputs = afterInputs(root, name, cfg, r.Inputs.Before)
	if errors.Is(runErr, exec.ErrWaitDelay) || (runtime.GOOS == "windows" && (result == receipt.ResultCancelled || result == receipt.ResultTimeout)) {
		reason := "owner_lost"
		r.Inputs.After = nil
		r.Inputs.Validity = receipt.InputsUnreadable
		r.Inputs.Reason = &reason
	}
	_, writeErr := fmt.Fprintf(logFile, "\nnaru: result %s, exit %d, inputs %s\n", result, exitCode, r.Inputs.Validity)
	syncErr := logFile.Sync()
	closed = true
	closeErr := logFile.Close()
	if err := errors.Join(out.err, writeErr, syncErr, closeErr); err != nil {
		return Outcome{}, fmt.Errorf("finish check log: %w", err)
	}
	if err := store.Put(r); err != nil {
		return Outcome{}, fmt.Errorf("save execution result: %w", err)
	}
	outcome = Outcome{Receipt: legacyView(r), Record: &r, Tail: tail.String()}
	if result == receipt.ResultCancelled {
		return outcome, ErrInterrupted
	}
	return outcome, nil
}

func inputDigest(d fileset.Digest) receipt.InputDigest {
	return receipt.InputDigest{Hash: d.Hash, FileCount: d.FileCount, FileSource: string(d.Source)}
}
func afterInputs(root, name string, beforeConfig *config.Config, before receipt.InputDigest) receipt.Inputs {
	i := receipt.Inputs{Policy: receipt.InputPolicy, Before: before, Validity: receipt.InputsUnreadable}
	reason := "definition_unreadable"
	i.Reason = &reason
	cfg, err := config.Load(root)
	if err != nil {
		return i
	}
	ch, ok := cfg.Checks[name]
	if !ok {
		return i
	}
	reason = "input_read_failed"
	after, err := fileset.Compute(root, ch.Inputs, cfg.Exclude, cfg.Fingerprint(name))
	if err != nil {
		return i
	}
	if after.FileCount == 0 {
		reason = "empty_inputs"
		return i
	}
	d := inputDigest(after)
	i.After = &d
	i.Validity = receipt.InputsMatched
	i.Reason = nil
	if d.Hash != before.Hash || d.FileCount != before.FileCount {
		reason = "inputs_changed"
		if cfg.Fingerprint(name) != beforeConfig.Fingerprint(name) {
			reason = "definition_changed"
		}
		i.Validity = receipt.InputsChanged
		i.Reason = &reason
	}
	return i
}

// legacyView is only an in-memory display adapter, never persisted as v1 evidence.
func legacyView(r receipt.Record) receipt.Receipt {
	out := receipt.Receipt{Schema: receipt.StateSchema, Check: r.Check, Command: r.Execution.Command, InputsHash: r.Inputs.Before.Hash, FileCount: r.Inputs.Before.FileCount, FileSource: r.Inputs.Before.FileSource, Evidence: r.Provenance.Kind, StartedAt: r.StartedAt, LogPath: r.Execution.LogPath, NaruVersion: r.Execution.NaruVersion}
	if r.FinishedAt != nil {
		out.FinishedAt = *r.FinishedAt
	}
	if r.Execution.Result != nil {
		out.Result = *r.Execution.Result
	}
	if r.Execution.ExitCode != nil {
		out.ExitCode = *r.Execution.ExitCode
	}
	if r.Execution.DurationMS != nil {
		out.DurationMS = *r.Execution.DurationMS
	}
	return out
}
