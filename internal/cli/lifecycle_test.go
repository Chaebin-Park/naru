package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/status"
)

// TestCLIExecutionCheckProcess is the single process used by the lifecycle
// fixture. It announces readiness after exec, without spawning polling children.
func TestCLIExecutionCheckProcess(t *testing.T) {
	if os.Getenv("NARU_LIFECYCLE_HELPER") != "1" {
		return
	}
	if _, err := os.Stat(".naru/wait"); err == nil {
		parent := os.Getppid()
		if err := os.WriteFile(".naru/ready", []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-deadline.C:
				t.Fatal("fixture was not cancelled before its deadline")
			case <-tick.C:
				if os.Getppid() != parent {
					t.Fatal("owning CLI exited without cancelling the fixture")
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	data, err := os.ReadFile("src/value.txt")
	if err != nil || !bytes.Contains(data, []byte("ok")) {
		t.Fatalf("fixture input: %q %v", data, err)
	}
}

func TestCLIExecutionLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix SIGINT process-group integration")
	}
	bin := filepath.Join(t.TempDir(), "naru")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/naru") // #nosec G204 -- Builds the current CLI into a test-owned directory.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	root := newProject(t, false)
	// exec leaves one process in the check group. The helper owns the barrier,
	// so cancellation cannot race a polling shell's creation of a sleep child.
	quotedHelper := "'" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "'"
	command := "NARU_LIFECYCLE_HELPER=1 exec " + quotedHelper + " -test.run=^TestCLIExecutionCheckProcess$"
	quoted, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks: {c: {run: "+string(quoted)+", inputs: ['src/**'], timeout: 10s}}\n")
	write(t, root, "src/value.txt", "ok")
	invoke := func(dir string, args ...string) (int, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- Test-built executable with test-controlled argument lists.
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "NARU_MODE=", "NARU_GATE=", "CLAUDE_PROJECT_DIR=")
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		return exit.ExitCode(), string(out)
	}
	if code, out := invoke(root, "check"); code != 0 {
		t.Fatalf("initial check %d: %s", code, out)
	}
	first, err := receipt.NewStateStore(root).Current("c")
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".naru/wait", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owner := exec.CommandContext(ctx, bin, "check") // #nosec G204 -- Test-built CLI for cancellation integration.
	owner.WaitDelay = 2 * time.Second
	owner.Dir = root
	owner.Env = append(os.Environ(), "NARU_MODE=")
	var out bytes.Buffer
	owner.Stdout = &out
	owner.Stderr = &out
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			// Let the CLI cancel and reap its check on assertion failures too.
			// CommandContext still bounds this wait if the CLI cannot respond.
			_ = owner.Process.Signal(os.Interrupt)
			_ = owner.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, ".naru/ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("check did not reach barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, err := receipt.NewStateStore(root).Current("c")
	if err != nil {
		t.Fatal(err)
	}
	if code, out := invoke(root, "check"); code != 1 || !strings.Contains(out, "already running") {
		t.Fatalf("duplicate: %d %s", code, out)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if code, out := invoke(alias, "check"); code != 1 {
		t.Fatalf("alias duplicate: %d %s", code, out)
	}
	same, err := receipt.NewStateStore(root).Current("c")
	if err != nil || same.RunID != current.RunID {
		t.Fatalf("duplicate changed owner: %v", err)
	}
	if code, out := invoke(root, "status", "--json"); code != 1 || !strings.Contains(out, `"state": "running"`) {
		t.Fatalf("active status: %d %s", code, out)
	}
	if err := owner.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = owner.Wait()
	waited = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 130 {
		t.Fatalf("cancel: %v %s", err, &out)
	}
	code, raw := invoke(root, "status", "--json")
	var rep status.Report
	if err := json.Unmarshal([]byte(raw), &rep); err != nil {
		t.Fatalf("status JSON: %v %s", err, raw)
	}
	if code != 1 || rep.Schema != 2 || rep.Satisfied || rep.Checks[0].State != status.Cancelled {
		t.Fatalf("old pass reused: %d %+v", code, rep)
	}
	if _, err := os.Stat(filepath.Join(root, ".naru/state/history/c", first.RunID+".json")); err != nil {
		t.Fatal("previous pass not archived", err)
	}
	for _, gate := range []struct {
		name string
		code int
	}{{"stop", 2}, {"pre-push", 1}} {
		if code, out := invoke(root, "gate", gate.name); code != gate.code || !strings.Contains(out, "cancelled") {
			t.Fatalf("gate %s: %d %s", gate.name, code, out)
		}
	}
	if err := os.Remove(filepath.Join(root, ".naru/wait")); err != nil {
		t.Fatal(err)
	}
	if code, out := invoke(root, "check"); code != 0 {
		t.Fatalf("recovery: %d %s", code, out)
	}
	if code, out := invoke(root, "status"); code != 0 {
		t.Fatalf("recovered status: %d %s", code, out)
	}
}
