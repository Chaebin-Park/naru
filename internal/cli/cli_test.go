package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/status"
)

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, dir, stdin string, args ...string) result {
	t.Helper()
	t.Setenv("CLAUDE_PROJECT_DIR", "") // the test may itself run under Claude Code
	t.Setenv("NARU_GATE", "")
	t.Setenv("NARU_MODE", "")
	var out, errb bytes.Buffer
	code := Run(args, Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb, Dir: dir})
	return result{code, out.String(), errb.String()}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil { // #nosec G703 -- Test callers supply fixture-relative paths in a test-owned directory; no external input.
		t.Fatal(err)
	}
}

const projectConfig = `version: 1
default_mode: code
modes:
  code: [has-ok]
  docs: [has-readme]
checks:
  has-ok:
    run: grep -q ok src/value.txt
    inputs: ["src/**"]
  has-readme:
    run: test -f README.md
    inputs: ["*.md"]
`

func newProject(t *testing.T, useGit bool) string {
	t.Helper()
	root := t.TempDir()
	if useGit {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}
		cmd := exec.Command("git", "init", "-q")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
	}
	write(t, root, ".naru/config.yaml", projectConfig)
	write(t, root, "src/value.txt", "bad\n")
	return root
}

func stopPayload(cwd string) string {
	b, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "cwd": cwd, "stop_hook_active": false})
	return string(b)
}

// TestStopGateFlow walks the core contract: not-run, failed and stale checks all
// block; only a passing receipt for the current inputs lets the agent finish.
func TestStopGateFlow(t *testing.T) {
	for _, useGit := range []bool{true, false} {
		name := map[bool]string{true: "git", false: "plain-dir"}[useGit]
		t.Run(name, func(t *testing.T) {
			root := newProject(t, useGit)
			sub := filepath.Join(root, "src") // commands work from subdirectories too

			r := run(t, sub, stopPayload(sub), "gate", "stop")
			if r.code != 2 || !strings.Contains(r.stderr, "has-ok: never run") {
				t.Fatalf("missing receipt must block: %+v", r)
			}

			r = run(t, sub, "", "check")
			if r.code != 1 || !strings.Contains(r.stdout, "FAIL has-ok exit=1") {
				t.Fatalf("failing check: %+v", r)
			}
			r = run(t, sub, stopPayload(sub), "gate", "stop")
			if r.code != 2 || !strings.Contains(r.stderr, "has-ok: fail (exit 1) running `grep -q ok src/value.txt`") {
				t.Fatalf("failed receipt must block: %+v", r)
			}

			write(t, root, "src/value.txt", "ok\n")
			r = run(t, sub, stopPayload(sub), "gate", "stop")
			if r.code != 2 || !strings.Contains(r.stderr, "has-ok: inputs or check definition changed") {
				t.Fatalf("fixing without re-running must block as stale: %+v", r)
			}

			r = run(t, sub, "", "check")
			if r.code != 0 || !strings.Contains(r.stdout, "PASS has-ok") {
				t.Fatalf("passing check: %+v", r)
			}
			r = run(t, sub, stopPayload(sub), "gate", "stop")
			if r.code != 0 || r.stderr != "" {
				t.Fatalf("passing receipt must allow: %+v", r)
			}
			r = run(t, root, "", "status")
			if r.code != 0 || !strings.Contains(r.stdout, "done:") {
				t.Fatalf("status after pass: %+v", r)
			}

			write(t, root, "src/value.txt", "ok, edited later\n")
			r = run(t, root, "", "status", "--json")
			var rep status.Report
			if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
				t.Fatalf("status --json: %v\n%s", err, r.stdout)
			}
			if r.code != 1 || rep.Satisfied || rep.Checks[0].State != status.Stale {
				t.Fatalf("edit after pass must be stale: code=%d %+v", r.code, rep)
			}

			// Files outside the check's inputs do not invalidate it.
			run(t, root, "", "check")
			write(t, root, "unrelated.log", "noise")
			if r := run(t, root, stopPayload(root), "gate", "stop"); r.code != 0 {
				t.Fatalf("unrelated file blocked: %+v", r)
			}
		})
	}
}

func TestModeSelectsRequiredChecks(t *testing.T) {
	root := newProject(t, false)
	if r := run(t, root, "", "mode", "docs"); r.code != 0 {
		t.Fatalf("mode docs: %+v", r)
	}
	r := run(t, root, stopPayload(root), "gate", "stop")
	if r.code != 2 || !strings.Contains(r.stderr, `mode "docs"`) || !strings.Contains(r.stderr, "has-readme") {
		t.Fatalf("stored mode not applied: %+v", r)
	}
	write(t, root, "README.md", "# x")
	run(t, root, "", "check")
	if r := run(t, root, stopPayload(root), "gate", "stop"); r.code != 0 {
		t.Fatalf("docs mode should pass without has-ok: %+v", r)
	}
	if r := run(t, root, "", "mode", "nope"); r.code != 64 {
		t.Fatalf("unknown mode accepted: %+v", r)
	}
	run(t, root, "", "mode", "--clear")
	if r := run(t, root, "", "mode"); !strings.HasPrefix(r.stdout, "code (default)") {
		t.Fatalf("clear did not restore default: %+v", r)
	}
}

func TestGateOutsideProjectAllowsAndBrokenConfigBlocks(t *testing.T) {
	plain := t.TempDir()
	if r := run(t, plain, stopPayload(plain), "gate", "stop"); r.code != 0 {
		t.Fatalf("non-naru directory must not be blocked: %+v", r)
	}
	write(t, plain, ".naru/config.yaml", "version: 1\nchecks: {}\n")
	r := run(t, plain, stopPayload(plain), "gate", "stop")
	if r.code != 2 || !strings.Contains(r.stderr, "invalid") {
		t.Fatalf("broken config must block: %+v", r)
	}
	// Garbage stdin falls back to the working directory.
	if r := run(t, plain, "not json", "gate", "stop"); r.code != 2 {
		t.Fatalf("garbage payload: %+v", r)
	}
}

func TestCheckTimeoutKillsProcess(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".naru/config.yaml", `version: 1
default_mode: code
modes: {code: [slow]}
checks:
  slow:
    run: sleep 30
    inputs: ["**"]
    timeout: 300ms
`)
	write(t, root, "input.txt", "x")
	r := run(t, root, "", "check")
	if r.code != 1 || !strings.Contains(r.stdout, "TIMEOUT slow") {
		t.Fatalf("timeout not reported: %+v", r)
	}
}

func TestInitDetectsStackAndRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/x\n")
	r := run(t, root, "", "init")
	if r.code != 0 || !strings.Contains(r.stdout, "detected go") {
		t.Fatalf("init: %+v", r)
	}
	cfg, _ := os.ReadFile(filepath.Join(root, ".naru/config.yaml")) // #nosec G304 -- Reads a synthetic fixture or a file in the test-owned temporary directory.
	if !strings.Contains(string(cfg), `"go test ./..."`) || !strings.Contains(string(cfg), `inputs: ["**"]`) {
		t.Fatalf("go check missing:\n%s", cfg)
	}
	if gi, _ := os.ReadFile(filepath.Join(root, ".naru/.gitignore")); !strings.Contains(string(gi), "receipts/") || !strings.Contains(string(gi), "relay/") { // #nosec G304 -- Reads a synthetic fixture or a file in the test-owned temporary directory.
		t.Fatalf(".gitignore missing private evidence paths: %q", gi)
	}
	if r := run(t, root, "", "init"); r.code != 64 {
		t.Fatalf("second init must refuse: %+v", r)
	}
	if r := run(t, root, "", "status"); r.code != 1 || !strings.Contains(r.stdout, "missing") {
		t.Fatalf("generated config must load and start unsatisfied: %+v", r)
	}
}

func TestInitWithoutStackFailsClosed(t *testing.T) {
	root := t.TempDir()
	write(t, root, "notes.txt", "x")
	if r := run(t, root, "", "init"); r.code != 0 {
		t.Fatalf("init: %+v", r)
	}
	if r := run(t, root, "", "check"); r.code != 1 {
		t.Fatalf("placeholder check must fail: %+v", r)
	}
}

func TestUsageErrors(t *testing.T) {
	dir := t.TempDir()
	if r := run(t, dir, "", "status"); r.code != 64 || !strings.Contains(r.stderr, "naru init") {
		t.Fatalf("status outside project: %+v", r)
	}
	if r := run(t, dir, ""); r.code != 64 {
		t.Fatalf("no args: %+v", r)
	}
	if r := run(t, dir, "", "bogus"); r.code != 64 {
		t.Fatalf("unknown command: %+v", r)
	}
	root := newProject(t, false)
	if r := run(t, root, "", "check", "ghost"); r.code != 64 {
		t.Fatalf("unknown check: %+v", r)
	}
	if r := run(t, root, "", "check", "--evidence", "bogus"); r.code != 64 {
		t.Fatalf("bad evidence: %+v", r)
	}
}

func TestGateExplainsRemovedStoredMode(t *testing.T) {
	root := newProject(t, false)
	run(t, root, "", "mode", "docs")
	write(t, root, ".naru/config.yaml", strings.Replace(projectConfig, "  docs: [has-readme]\n", "", 1))
	r := run(t, root, stopPayload(root), "gate", "stop")
	if r.code != 2 || !strings.Contains(r.stderr, "naru mode --clear") {
		t.Fatalf("removed stored mode: %+v", r)
	}
}
