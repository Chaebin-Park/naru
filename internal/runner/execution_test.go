package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

func stateProject(t *testing.T, command string) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".naru"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateWrite(t, root, "in.txt", "original")
	return root, stateConfig(t, root, command)
}
func stateConfig(t *testing.T, root, command string) *config.Config {
	t.Helper()
	quoted, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	stateWrite(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks:\n  c:\n    inputs: [in.txt]\n    timeout: 10s\n    run: "+string(quoted)+"\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func stateWrite(t *testing.T, root, path, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
func stateRun(root string, cfg *config.Config, opt Options) (Outcome, error) {
	return runState(root, cfg, "c", opt, func(path string) (logSink, error) {
		return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- Generated log path in a test-owned temporary project.
	})
}
func waitFile(t *testing.T, root, path string) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(filepath.Join(root, path)); err == nil {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("barrier %s not reached", path)
		case <-tick.C:
		}
	}
}

const barrierCommand = "echo start >> starts; touch ready; while [ ! -f release ]; do sleep 0.01; done; touch finished"

func TestExecutionProcess(t *testing.T) {
	if os.Getenv("NARU_EXEC_HELPER") != "1" {
		return
	}
	root := os.Getenv("NARU_EXEC_ROOT")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
}
func executionProcess(t *testing.T, root string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestExecutionProcess$") // #nosec G204 G702 -- Fixed helper in this test binary with a temporary project.
	cmd.Env = append(os.Environ(), "NARU_EXEC_HELPER=1", "NARU_EXEC_ROOT="+root)
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}
func TestDuplicateExecutionCannotChangeOwner(t *testing.T) {
	root, cfg := stateProject(t, barrierCommand)
	cmd := executionProcess(t, root)
	done := false
	t.Cleanup(func() {
		stateWrite(t, root, "release", "")
		if !done {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	waitFile(t, root, "ready")
	old, err := receipt.NewStateStore(root).Current("c")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stateRun(root, cfg, Options{}); !errors.Is(err, runlock.ErrBusy) {
		t.Fatalf("duplicate: %v", err)
	}
	current, err := receipt.NewStateStore(root).Current("c")
	if err != nil || current.RunID != old.RunID || current.Phase != receipt.PhaseRunning {
		t.Fatalf("owner overwritten: %+v %v", current, err)
	}
	stateWrite(t, root, "release", "")
	if err := cmd.Wait(); err != nil {
		t.Fatalf("%v: %s %s", err, cmd.Stdout, cmd.Stderr)
	}
	done = true
	starts, err := os.ReadFile(filepath.Join(root, "starts")) // #nosec G304 -- Test-owned command start counter.
	if err != nil {
		t.Fatal(err)
	}
	if string(starts) != "start\n" {
		t.Fatalf("started twice: %s", starts)
	}
}
func TestKilledOwnerDoesNotPermitOrphanOverlap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix parent-kill subprocess behavior")
	}
	root, cfg := stateProject(t, barrierCommand)
	cmd := executionProcess(t, root)
	done := false
	t.Cleanup(func() {
		stateWrite(t, root, "release", "")
		if !done {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	waitFile(t, root, "ready")
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	done = true
	// The shell is still waiting for release, but the owner's OS lock is gone.
	if _, err := stateRun(root, cfg, Options{}); !errors.Is(err, ErrRecovery) {
		t.Fatalf("orphan overlap: %v", err)
	}
	stateWrite(t, root, "release", "")
	waitFile(t, root, "finished")
	if _, err := stateRun(root, cfg, Options{}); !errors.Is(err, ErrRecovery) {
		t.Fatalf("guessed automatic recovery: %v", err)
	}
}
func TestCancelledExecutionArchivesPass(t *testing.T) {
	root, cfg := stateProject(t, "true")
	first, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cfg = stateConfig(t, root, barrierCommand)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := stateRun(root, cfg, Options{Context: ctx}); done <- err }()
	waitFile(t, root, "ready")
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupted) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation hung")
	}
	r, err := receipt.NewStateStore(root).Current("c")
	if err != nil || r.Phase != receipt.PhaseTerminal || *r.Execution.Result != receipt.ResultCancelled {
		t.Fatalf("old pass reused: %+v %v", r, err)
	}
	history := filepath.Join(root, ".naru/state/history/c", first.Record.RunID+".json")
	data, err := os.ReadFile(history) // #nosec G304 -- Test-owned archived receipt.
	if err != nil {
		t.Fatal(err)
	}
	archived, err := receipt.DecodeRecord(data, "c")
	if err != nil || *archived.Execution.Result != receipt.ResultPass {
		t.Fatalf("lost original: %v", err)
	}
	cfg = stateConfig(t, root, "true")
	if _, err := stateRun(root, cfg, Options{}); err != nil {
		t.Fatal(err)
	}
}
func TestCancelledBeforeLockPreservesState(t *testing.T) {
	root, cfg := stateProject(t, "true")
	first, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := stateRun(root, cfg, Options{Context: ctx}); !errors.Is(err, ErrInterrupted) {
		t.Fatal(err)
	}
	r, err := receipt.NewStateStore(root).Current("c")
	if err != nil || r.RunID != first.Record.RunID {
		t.Fatalf("cancelled request changed state: %v", err)
	}
}
func TestExecutionInputValidity(t *testing.T) {
	for _, tc := range []struct{ name, command, validity, reason string }{
		{"content", "echo changed > in.txt", receipt.InputsChanged, "inputs_changed"},
		{"mode", "chmod +x in.txt", receipt.InputsChanged, "inputs_changed"},
		{"empty", "rm in.txt", receipt.InputsUnreadable, "empty_inputs"},
		{"config", "echo broken > .naru/config.yaml", receipt.InputsUnreadable, "definition_unreadable"},
		{"definition", "sed 's/timeout: 10s/timeout: 11s/; s/inputs: \\[in.txt\\]/inputs: [in.txt, extra.txt]/' .naru/config.yaml > next; mv next .naru/config.yaml", receipt.InputsChanged, "definition_changed"},
		{"ABA", "printf changed > in.txt; grep -q changed in.txt; printf original > in.txt", receipt.InputsMatched, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, cfg := stateProject(t, tc.command)
			out, err := stateRun(root, cfg, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if out.Receipt.Result != receipt.ResultPass {
				t.Fatalf("command result lost: %+v", out)
			}
			if out.Record.Inputs.Validity != tc.validity {
				t.Fatalf("validity: %+v", out.Record.Inputs)
			}
			if tc.reason != "" && (out.Record.Inputs.Reason == nil || *out.Record.Inputs.Reason != tc.reason) {
				t.Fatalf("reason: %+v", out.Record.Inputs)
			}
		})
	}
}

type failingCompletion struct{ receipt.StateStore }

func (s failingCompletion) Put(r receipt.Record) error {
	if r.Phase == receipt.PhaseTerminal {
		return fmt.Errorf("injected completion storage failure")
	}
	return s.StateStore.Put(r)
}
func TestCompletionFailureLeavesRunningAuthority(t *testing.T) {
	root, cfg := stateProject(t, "true")
	first, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{store: func(root string) executionStore { return failingCompletion{receipt.NewStateStore(root)} }}
	if _, err := stateRun(root, cfg, opt); err == nil {
		t.Fatal("ignored completion failure")
	}
	r, err := receipt.NewStateStore(root).Current("c")
	if err != nil || r.Phase != receipt.PhaseRunning || r.RunID == first.Record.RunID {
		t.Fatalf("old pass returned: %+v %v", r, err)
	}
}
func TestMissingShellRecordsError(t *testing.T) {
	root, cfg := stateProject(t, "true")
	t.Setenv("PATH", t.TempDir())
	out, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.Execution.Result == nil || *out.Record.Execution.Result != receipt.ResultError {
		t.Fatalf("missing shell: %+v", out.Record)
	}
}
func TestStartFailureNeverExecutesCommand(t *testing.T) {
	root, cfg := stateProject(t, "touch started")
	stateWrite(t, root, ".naru/state", "not a directory")
	if _, err := stateRun(root, cfg, Options{}); err == nil {
		t.Fatal("accepted broken state directory")
	}
	if _, err := os.Stat(filepath.Join(root, "started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("started without persistent authority")
	}
}

func TestLegacyOriginalPreserved(t *testing.T) {
	root, cfg := stateProject(t, "true")
	old := receipt.Receipt{Schema: 1, Check: "c", Command: "original", InputsHash: "sha256:" + strings.Repeat("a", 64), FileCount: 1, FileSource: "walk", Result: "pass", Evidence: "local", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), NaruVersion: "old", LogPath: "old.log"}
	if err := receipt.NewStore(root).Put(old); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".naru/receipts/c.json")) // #nosec G304 -- Test-owned legacy receipt.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stateRun(root, cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, ".naru/receipts/c.json")) // #nosec G304 -- Test log path supplied by the runner in a temporary project. // #nosec G304 -- Test-owned legacy receipt.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("legacy original changed")
	}
}

func TestInputSetAdditionAndDeletion(t *testing.T) {
	for _, command := range []string{"echo new > extra.txt", "rm extra.txt"} {
		t.Run(command, func(t *testing.T) {
			root, _ := stateProject(t, command)
			stateWrite(t, root, "extra.txt", "old")
			quoted, err := json.Marshal(command)
			if err != nil {
				t.Fatal(err)
			}
			stateWrite(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks: {c: {inputs: ['*.txt'], run: "+string(quoted)+"}}")
			if strings.HasPrefix(command, "echo") {
				if err := os.Remove(filepath.Join(root, "extra.txt")); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			out, err := stateRun(root, cfg, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if out.Record.Inputs.Validity != receipt.InputsChanged || out.Receipt.Result != receipt.ResultPass {
				t.Fatalf("set change accepted: %+v", out.Record)
			}
		})
	}
}
func TestUnreadableAfterInputs(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires non-root Unix permission enforcement")
	}
	root, cfg := stateProject(t, "chmod 000 in.txt")
	t.Cleanup(func() {
		if err := os.Chmod(filepath.Join(root, "in.txt"), 0o600); err != nil {
			t.Error(err)
		}
	})
	out, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.Inputs.Validity != receipt.InputsUnreadable || *out.Record.Inputs.Reason != "input_read_failed" || out.Receipt.Result != receipt.ResultPass {
		t.Fatalf("unreadable inputs: %+v", out.Record)
	}
}
