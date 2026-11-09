//go:build windows

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

// TestWindowsTreeProcess is a bounded, cooperative child/grandchild fixture.
// Parent-held native handles, rather than reusable PID numbers, govern cleanup.
func TestWindowsTreeProcess(t *testing.T) {
	role := os.Getenv("NARU_WINDOWS_TREE")
	if role == "" {
		return
	}
	if role != "child" && role != "grandchild" {
		t.Fatal("unknown fixture role")
	}
	stateWrite(t, ".", role+".pid", strconv.Itoa(os.Getpid()))
	if role == "child" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsTreeProcess$") // #nosec G204 G702 -- Fixed helper in this test executable; no external command input.
		cmd.Env = append(os.Environ(), "NARU_WINDOWS_TREE=grandchild")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := cmd.Wait(); err != nil {
				t.Error(err)
			}
		}()
	}
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat("release"); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fixture release deadline exceeded")
}

func TestWindowsCancellationRecovery(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "naru.exe")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/naru") // #nosec G204 -- Builds the repository CLI into a test-owned temporary directory.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, kind := range []string{"cancel", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			quoted := "'" + strings.ReplaceAll(filepath.ToSlash(os.Args[0]), "'", "'\\''") + "'"
			command := "if [ ! -f tree-mode ]; then exit 0; fi; NARU_WINDOWS_TREE=child " + quoted + " -test.run=^TestWindowsTreeProcess$"
			root, cfg := stateProject(t, command)
			assertStatus := func(satisfied bool) {
				t.Helper()
				cmd := exec.Command(binary, "status", "--json") // #nosec G204 -- Test-built CLI with fixed arguments.
				cmd.Dir = root
				out, err := cmd.CombinedOutput()
				wantExit := 1
				if satisfied {
					wantExit = 0
				}
				code := 0
				if err != nil {
					var exit *exec.ExitError
					if !errors.As(err, &exit) {
						t.Fatal(err)
					}
					code = exit.ExitCode()
				}
				var report struct {
					Satisfied bool `json:"satisfied"`
				}
				if err := json.Unmarshal(out, &report); err != nil || code != wantExit || report.Satisfied != satisfied {
					t.Fatalf("status: exit=%d JSON=%s err=%v", code, out, err)
				}
			}
			first, err := stateRun(root, cfg, Options{})
			if err != nil || first.Record == nil || *first.Record.Execution.Result != receipt.ResultPass {
				t.Fatalf("initial pass: %+v %v", first, err)
			}
			stateWrite(t, root, "tree-mode", "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := stateRun(root, cfg, Options{Context: ctx}); done <- err }()
			var handles []syscall.Handle
			// Release all known fixtures even if an assertion fails. Open handles pin
			// process identities across cancellation and prevent PID-reuse mistakes.
			cleaned := false
			cleanup := func() {
				if cleaned {
					return
				}
				cleaned = true
				stateWrite(t, root, "release", "")
				for _, h := range handles {
					state, err := syscall.WaitForSingleObject(h, 5000)
					if err != nil || state != syscall.WAIT_OBJECT_0 {
						// Only these live-test handles can be terminated, never a
						// fresh handle reconstructed from stale recovery metadata.
						if err := syscall.TerminateProcess(h, 1); err != nil {
							t.Errorf("fixture termination: %v", err)
						}
						state, err = syscall.WaitForSingleObject(h, 5000)
					}
					if err != nil || state != syscall.WAIT_OBJECT_0 {
						t.Errorf("fixture remains alive: state=%d err=%v", state, err)
					}
					t.Logf("cleanup: process handle signaled=%t", state == syscall.WAIT_OBJECT_0 && err == nil)
				}
			}
			defer func() {
				cancel()
				cleanup()
				for _, h := range handles {
					if err := syscall.CloseHandle(h); err != nil {
						t.Error(err)
					}
				}
			}()
			for _, role := range []string{"child", "grandchild"} {
				waitFile(t, root, role+".pid")
				data, err := os.ReadFile(filepath.Join(root, role+".pid")) // #nosec G304 -- Fixed fixture role names below a test-owned temporary root.
				if err != nil {
					t.Fatal(err)
				}
				pid, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
				if err != nil || pid <= 1 {
					t.Fatalf("invalid fixture PID: %s %v", data, err)
				}
				h, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_TERMINATE, false, uint32(pid))
				if err != nil {
					t.Fatal(err)
				}
				handles = append(handles, h)
				t.Logf("%s started: pid=%d", role, pid)
			}
			current, err := receipt.NewStateStore(root).Current("c")
			if err != nil || current.Phase != receipt.PhaseRunning {
				t.Fatalf("expected running: %+v %v", current, err)
			}
			if _, err := stateRun(root, cfg, Options{}); !errors.Is(err, runlock.ErrBusy) {
				t.Fatalf("duplicate execution: %v", err)
			}
			owner, err := receipt.NewStateStore(root).Current("c")
			if err != nil || owner.RunID != current.RunID || owner.Phase != receipt.PhaseRunning {
				t.Fatalf("duplicate overwrote owner: %+v %v", owner, err)
			}
			if kind == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if (kind == "cancel" && !errors.Is(err, ErrInterrupted)) || (kind == "timeout" && err != nil) {
					t.Fatalf("completion: %v", err)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("cancellation/timeout did not return")
			}
			for i, h := range handles {
				state, err := syscall.WaitForSingleObject(h, 0)
				if err != nil || (state != syscall.WAIT_OBJECT_0 && state != syscall.WAIT_TIMEOUT) {
					t.Fatalf("process observation: %d %v", state, err)
				}
				t.Logf("after %s: %s alive=%t", kind, []string{"child", "grandchild"}[i], state == syscall.WAIT_TIMEOUT)
			}
			r, err := receipt.NewStateStore(root).Current("c")
			want := receipt.ResultCancelled
			if kind == "timeout" {
				want = receipt.ResultTimeout
			}
			if err != nil || r.RunID == first.Record.RunID || r.Execution.Result == nil || *r.Execution.Result != want || r.Inputs.Validity != receipt.InputsUnreadable || r.Inputs.Reason == nil || *r.Inputs.Reason != "owner_lost" {
				t.Fatalf("prior pass or unsafe authority reused: %+v %v", r, err)
			}
			assertStatus(false)
			if _, err := stateRun(root, cfg, Options{}); !errors.Is(err, ErrRecovery) {
				t.Fatalf("restart before cleanup: %v", err)
			}
			cleanup()
			if t.Failed() {
				t.Fatal("cleanup failed; cannot claim recovery")
			}
			if _, err := stateRun(root, cfg, Options{}); !errors.Is(err, ErrRecovery) {
				t.Fatalf("cleanup silently cleared authority: %v", err)
			}
			if err := os.Rename(filepath.Join(root, ".naru/state/checks/c.json"), filepath.Join(root, "preserved-c.json")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(root, "tree-mode")); err != nil {
				t.Fatal(err)
			}
			out, err := stateRun(root, cfg, Options{})
			if err != nil || out.Record == nil || *out.Record.Execution.Result != receipt.ResultPass {
				t.Fatalf("recovery: %+v %v", out, err)
			}
			assertStatus(true)
			t.Logf("%s: prior pass blocked; concurrent start refused; owner_lost preserved; inspected cleanup and fresh rerun satisfied", kind)
		})
	}
}
