package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestCLIPathLifecycle exercises installed executable and input paths through
// real processes. Unlike the cancellation fixture, it also runs on Windows.
func TestCLIPathLifecycle(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Fatal("path lifecycle requires POSIX sh: ", err)
	}
	for _, name := range []string{"NARU_MODE", "NARU_GATE", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(name, "")
	}
	name := "naru"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	built := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", built, "../../cmd/naru").CombinedOutput(); err != nil { // #nosec G204 -- Builds the current CLI into a test-owned temporary directory with fixed arguments.
		t.Fatalf("build: %v\n%s", err, out)
	}
	binary, err := os.ReadFile(built) // #nosec G304 -- Reads only the executable just built into t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"space path", "한글 경로"} {
		t.Run(label, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), label)
			installed := filepath.Join(root, "bin", name)
			if err := os.MkdirAll(filepath.Dir(installed), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(installed, binary, 0o700); err != nil { // #nosec G703 G306 -- Fixed test labels below t.TempDir; execute permission is required for this installed CLI fixture.
				t.Fatal(err)
			}
			invoke := func(want int, args ...string) []byte {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, installed, args...) // #nosec G204 -- Runs the test-built executable with fixture-owned argument lists.
				cmd.Dir = root
				out, err := cmd.CombinedOutput()
				code := 0
				if err != nil {
					if exit, ok := err.(*exec.ExitError); ok {
						code = exit.ExitCode()
					} else {
						t.Fatalf("%v: %v", args, err)
					}
				}
				if code != want {
					t.Fatalf("%v: exit=%d want=%d\n%s", args, code, want, out)
				}
				return out
			}
			invoke(0, "version")
			invoke(0, "init")
			write(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: code\nmodes: {code: [content]}\nchecks:\n  content:\n    run: 'test \"$(cat src/value.txt)\" = ok && test -f \"src/한글 이름.txt\"'\n    inputs: ['src/**']\n")
			write(t, root, "src/value.txt", "ok\n")
			write(t, root, "src/한글 이름.txt", "input\n")
			write(t, root, "excluded.txt", "outside declared inputs\n")
			status := func(want int, state string) {
				t.Helper()
				out := invoke(want, "status", "--json")
				var report struct {
					Satisfied bool `json:"satisfied"`
					Checks    []struct {
						State  string `json:"state"`
						Record *struct {
							Inputs struct {
								After struct {
									FileCount int `json:"file_count"`
								} `json:"after"`
							} `json:"inputs"`
						} `json:"record"`
					} `json:"checks"`
				}
				if err := json.Unmarshal(out, &report); err != nil {
					t.Fatalf("status JSON: %v\n%s", err, out)
				}
				if report.Satisfied != (want == 0) || len(report.Checks) != 1 || report.Checks[0].State != state {
					t.Fatalf("want %s: %s", state, out)
				}
				if state == "pass" && (report.Checks[0].Record == nil || report.Checks[0].Record.Inputs.After.FileCount != 2) {
					t.Fatalf("expected exactly the two declared input files: %s", out)
				}
				t.Logf("%s: exit=%d state=%s satisfied=%t", label, want, state, report.Satisfied)
			}
			status(1, "missing")
			invoke(0, "check")
			status(0, "pass")
			write(t, root, "excluded.txt", "unrelated change\n")
			status(0, "pass")
			write(t, root, "src/한글 이름.txt", "changed\n")
			status(1, "stale")
			invoke(0, "check")
			status(0, "pass")
			write(t, root, "src/value.txt", "bad\n")
			invoke(1, "check")
			status(1, "fail")
			write(t, root, "src/value.txt", "ok\n")
			invoke(0, "check")
			status(0, "pass")
		})
	}
}
