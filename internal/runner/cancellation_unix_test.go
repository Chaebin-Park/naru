//go:build unix

package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/receipt"
)

// The child remains in the shell's group and keeps its output descriptors open.
const cancellationChildCommand = "sleep 30 & echo $! > child; touch ready; wait"

func recordedChild(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "child")) // #nosec G304 -- Test-owned root and fixed child PID fixture.
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		t.Fatalf("invalid child pid %q: %v", data, err)
	}
	return pid
}

func cleanRecordedChild(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := os.Stat(filepath.Join(root, "child")); errors.Is(err, os.ErrNotExist) {
			return
		}
		pid := recordedChild(t, root)
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("clean child %d: %v", pid, err)
		}
	})
}

// Model a group signal missing a child without changing global process functions.
func runMissingChild(cmd *exec.Cmd) error {
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	return cmd.Run()
}

func assertChildPresent(t *testing.T, root string) {
	t.Helper()
	if err := syscall.Kill(recordedChild(t, root), 0); err != nil {
		t.Fatalf("fixture did not leave a child: %v", err)
	}
}

func assertSurfaceRecovery(t *testing.T, root string) {
	t.Helper()
	state, err := (baseline.Store{Root: root}).Current("api")
	if err != nil || state == nil || state.Phase != "recovery" || state.Reason != "owner_lost" || state.Snapshot != nil {
		t.Fatalf("unaccounted extractor child: %+v %v", state, err)
	}
	if _, err := Extract(root, "api", context.Background()); !errors.Is(err, ErrRecovery) {
		t.Fatalf("restarted surface: %v", err)
	}
}

func assertCheckRecovery(t *testing.T, root string) {
	t.Helper()
	state, err := receipt.NewStateStore(root).Current("c")
	if err != nil || state == nil || state.Inputs.Validity != receipt.InputsUnreadable || state.Inputs.Reason == nil || *state.Inputs.Reason != "owner_lost" {
		t.Fatalf("unaccounted check child: %+v %v", state, err)
	}
}

func TestSurfaceCancellationRecovery(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "nonzero_exit_with_child"} {
		t.Run(mode, func(t *testing.T) {
			root := surfaceProject(t)
			command := cancellationChildCommand
			if mode == "nonzero_exit_with_child" {
				command = "sleep 30 & echo $! > child; exit 1"
			}
			surfaceConfig(t, root, command, "")
			cleanRecordedChild(t, root)
			if mode == "timeout" {
				p := filepath.Join(root, ".naru/config.yaml")
				data, err := os.ReadFile(p) // #nosec G304 -- Fixed config file in a test-owned temporary root.
				if err != nil {
					t.Fatal(err)
				}
				stateWrite(t, root, ".naru/config.yaml", strings.Replace(string(data), "timeout: 2s", "timeout: 500ms", 1))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := extractWithRun(root, "api", ctx, runMissingChild); done <- err }()
			if mode == "cancel" {
				waitFile(t, root, "ready")
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, ErrRecovery) {
					t.Fatalf("missing recovery error: %v", err)
				}
				if mode == "cancel" && !errors.Is(err, ErrInterrupted) {
					t.Fatalf("missing interruption: %v", err)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("extraction did not return")
			}
			assertChildPresent(t, root)
			assertSurfaceRecovery(t, root)
		})
	}
}

func TestCheckCancellationRecovery(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "nonzero_exit_with_child"} {
		t.Run(mode, func(t *testing.T) {
			command := cancellationChildCommand
			if mode == "nonzero_exit_with_child" {
				command = "sleep 30 & echo $! > child; exit 1"
			}
			root, cfg := stateProject(t, command)
			cleanRecordedChild(t, root)
			if mode == "timeout" {
				p := filepath.Join(root, ".naru/config.yaml")
				data, err := os.ReadFile(p) // #nosec G304 -- Fixed config file in a test-owned temporary root.
				if err != nil {
					t.Fatal(err)
				}
				stateWrite(t, root, ".naru/config.yaml", strings.Replace(string(data), "timeout: 10s", "timeout: 500ms", 1))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			type result struct {
				out Outcome
				err error
			}
			done := make(chan result, 1)
			go func() {
				out, err := Run(root, cfg, "c", Options{Context: ctx, runCommand: runMissingChild})
				done <- result{out, err}
			}()
			if mode == "cancel" {
				waitFile(t, root, "ready")
				cancel()
			}
			select {
			case got := <-done:
				if mode == "cancel" && !errors.Is(got.err, ErrInterrupted) {
					t.Fatalf("cancel error: %v", got.err)
				}
				if mode != "cancel" && got.err != nil {
					t.Fatal(got.err)
				}
				want := receipt.ResultFail
				if mode == "cancel" {
					want = receipt.ResultCancelled
				}
				if mode == "timeout" {
					want = receipt.ResultTimeout
				}
				if got.out.Record == nil || got.out.Record.Execution.Result == nil || *got.out.Record.Execution.Result != want {
					t.Fatalf("result: %+v", got.out.Record)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("check did not return")
			}
			assertChildPresent(t, root)
			assertCheckRecovery(t, root)
			if _, err := Run(root, cfg, "c", Options{}); !errors.Is(err, ErrRecovery) {
				t.Fatalf("restarted check: %v", err)
			}
		})
	}
}

func TestProcessCleanupUncertainty(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pid       int
		responses []error
		want      bool
	}{
		{"absent", 123, []error{syscall.ESRCH}, false},
		{"exits_after_probe", 123, []error{nil, syscall.ESRCH}, false},
		{"survives", 123, []error{nil}, true},
		{"permission_denied", 123, []error{syscall.EPERM}, true},
		{"other_error", 123, []error{syscall.EIO}, true},
		{"invalid_pid", 0, nil, true},
		{"init_pid", 1, nil, true},
		{"negative_pid", -2, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			probe := func(pid int, signal syscall.Signal) error {
				if pid != -tc.pid || signal != 0 {
					t.Fatalf("unsafe probe: %d %v", pid, signal)
				}
				i := calls
				calls++
				if i >= len(tc.responses) {
					i = len(tc.responses) - 1
				}
				return tc.responses[i]
			}
			started := time.Now()
			if got := waitProcessGroup(tc.pid, probe, 30*time.Millisecond); got != tc.want {
				t.Fatalf("unresolved=%v want %v", got, tc.want)
			}
			if time.Since(started) > time.Second {
				t.Fatal("probe exceeded bound")
			}
			if tc.pid <= 1 && calls != 0 {
				t.Fatal("invalid pid was probed")
			}
		})
	}
}

func TestCancellationRecoveryAcrossProcess(t *testing.T) {
	if os.Getenv("NARU_RECOVERY_HELPER") == "1" {
		root := os.Getenv("NARU_RECOVERY_ROOT")
		switch os.Getenv("NARU_RECOVERY_KIND") {
		case "surface":
			surfaceConfig(t, root, "echo restarted > restarted", "")
			assertSurfaceRecovery(t, root)
		case "check":
			assertCheckRecovery(t, root)
			cfg := stateConfig(t, root, "echo restarted > restarted")
			if _, err := Run(root, cfg, "c", Options{}); !errors.Is(err, ErrRecovery) {
				t.Fatalf("fresh process restarted: %v", err)
			}
		default:
			t.Fatal("invalid helper kind")
		}
		return
	}
	for _, kind := range []string{"surface", "check"} {
		t.Run(kind, func(t *testing.T) {
			root := surfaceProject(t)
			cleanRecordedChild(t, root)
			command := "sleep 30 & echo $! > child; exit 1"
			if kind == "surface" {
				surfaceConfig(t, root, command, "")
				if _, err := extractWithRun(root, "api", context.Background(), runMissingChild); !errors.Is(err, ErrRecovery) {
					t.Fatal(err)
				}
			} else {
				cfg := stateConfig(t, root, "true")
				first, err := Run(root, cfg, "c", Options{})
				if err != nil {
					t.Fatal(err)
				}
				cfg = stateConfig(t, root, command)
				if _, err := Run(root, cfg, "c", Options{runCommand: runMissingChild}); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(root, ".naru/state/history/c", first.Record.RunID+".json")) // #nosec G304 -- Test-owned archived record selected by generated run ID.
				if err != nil {
					t.Fatal(err)
				}
				archived, err := receipt.DecodeRecord(data, "c")
				if err != nil || archived.Execution.Result == nil || *archived.Execution.Result != receipt.ResultPass {
					t.Fatalf("prior pass history lost: %+v %v", archived, err)
				}
			}
			helperCtx, helperCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer helperCancel()
			cmd := exec.CommandContext(helperCtx, os.Args[0], "-test.run=^TestCancellationRecoveryAcrossProcess$") // #nosec G204 G702 -- Fixed test-binary helper with test-owned paths.
			cmd.Env = append(os.Environ(), "NARU_RECOVERY_HELPER=1", "NARU_RECOVERY_ROOT="+root, "NARU_RECOVERY_KIND="+kind)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Run(); err != nil {
				t.Fatalf("fresh process recovery: %v\n%s", err, &output)
			}
			if _, err := os.Stat(filepath.Join(root, "restarted")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("recovery permitted execution: %v", err)
			}
		})
	}
}

// Normal group cancellation must account for the actual child, rather than
// checking a marker the child would only write after a thirty-second sleep.
func TestTimeoutKillsProcessGroup(t *testing.T)               { testTrackedGroupCancellation(t, true) }
func TestCancelKillsCheckAndRecordsCancellation(t *testing.T) { testTrackedGroupCancellation(t, false) }
func testTrackedGroupCancellation(t *testing.T, timeout bool) {
	t.Helper()
	root, cfg := stateProject(t, cancellationChildCommand)
	if timeout {
		p := filepath.Join(root, ".naru/config.yaml")
		data, err := os.ReadFile(p) // #nosec G304 -- Fixed config in a test-owned temporary root.
		if err != nil {
			t.Fatal(err)
		}
		stateWrite(t, root, ".naru/config.yaml", strings.Replace(string(data), "timeout: 10s", "timeout: 500ms", 1))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	type result struct {
		out Outcome
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := Run(root, cfg, "c", Options{Context: ctx}); done <- result{out, err} }()
	cleanupNeeded := true
	t.Cleanup(func() {
		if !cleanupNeeded {
			return
		}
		if _, err := os.Stat(filepath.Join(root, "child")); errors.Is(err, os.ErrNotExist) {
			return
		}
		pid := recordedChild(t, root)
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("clean child: %v", err)
		}
	})
	waitFile(t, root, "ready")
	pid := recordedChild(t, root)
	if !timeout {
		cancel()
	}
	var got result
	select {
	case got = <-done:
	case <-time.After(8 * time.Second):
		cancel()
		select {
		case got = <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("cancelled check exceeded bound")
	}
	want := receipt.ResultCancelled
	if timeout {
		want = receipt.ResultTimeout
	}
	if got.out.Record == nil || got.out.Record.Execution.Result == nil || *got.out.Record.Execution.Result != want {
		t.Fatalf("result %+v %v", got.out.Record, got.err)
	}
	if !timeout && !errors.Is(got.err, ErrInterrupted) {
		t.Fatalf("missing interruption: %v", got.err)
	}
	if timeout && got.err != nil {
		t.Fatal(got.err)
	}
	probe := syscall.Kill(pid, 0)
	if probe == nil {
		// A surviving child is allowed only with persisted recovery authority.
		assertCheckRecovery(t, root)
		if _, err := Run(root, cfg, "c", Options{}); !errors.Is(err, ErrRecovery) {
			t.Fatalf("survivor restarted: %v", err)
		}
	} else if !errors.Is(probe, syscall.ESRCH) {
		t.Fatalf("cannot check child: %v", probe)
	} else {
		cleanupNeeded = false
	}
}
