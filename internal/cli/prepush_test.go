package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrePushStates(t *testing.T) {
	root := newProject(t, true)
	assert := func(code int, message string) {
		t.Helper()
		r := run(t, root, "ignored git ref input", "gate", "pre-push")
		if r.code != code || !strings.Contains(r.stderr, message) {
			t.Fatalf("want %d %q: %+v", code, message, r)
		}
	}
	assert(1, "never run")
	if r := run(t, root, "", "check"); r.code != 1 {
		t.Fatal(r)
	}
	assert(1, "exit 1")
	write(t, root, "src/value.txt", "ok\n")
	assert(1, "changed")
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	assert(0, "")
	for _, record := range []string{`{"reason":"blocked"}`, "broken json", ""} {
		write(t, root, ".naru/state/unverified.json", record)
		assert(1, "NOT VERIFIED")
		if r := run(t, root, "", "gate", "stop"); r.code != 0 {
			t.Fatal(r)
		}
		assert(0, "")
	}
	write(t, root, ".naru/receipts/has-ok.json", "broken")
	assert(1, "cannot evaluate")
	if err := os.Remove(filepath.Join(root, "src/value.txt")); err != nil {
		t.Fatal(err)
	}
	// Restore a valid receipt to isolate the empty-input verdict.
	if err := os.Remove(filepath.Join(root, ".naru/receipts/has-ok.json")); err != nil {
		t.Fatal(err)
	}
	assert(1, "match no files")
	write(t, root, ".naru/config.yaml", "invalid")
	assert(1, "invalid config")
	if r := run(t, t.TempDir(), "", "gate", "pre-push"); r.code != 1 {
		t.Fatal(r)
	}
}

func TestPrePushIgnoresStopOverrides(t *testing.T) {
	root := newProject(t, false)
	write(t, root, "README.md", "docs")
	if r := run(t, root, "", "mode", "docs"); r.code != 0 {
		t.Fatal(r)
	}
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	t.Setenv("NARU_MODE", "docs")
	t.Setenv("NARU_GATE", "off")
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())
	for i := 0; i < 5; i++ {
		var out, errb bytes.Buffer
		code := Run([]string{"gate", "pre-push"}, Env{Dir: root, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb})
		if code != 1 || !strings.Contains(errb.String(), "has-ok: never run") {
			t.Fatalf("override bypass: %d %s", code, &errb)
		}
	}
}

func TestPrePushBrokenUnverifiedSymlink(t *testing.T) {
	root := newProject(t, false)
	write(t, root, "src/value.txt", "ok")
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	if err := os.MkdirAll(filepath.Join(root, ".naru/state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, ".naru/state/unverified.json")); err != nil {
		t.Skip(err)
	}
	if r := run(t, root, "", "gate", "pre-push"); r.code != 1 || !strings.Contains(r.stderr, "NOT VERIFIED") {
		t.Fatal(r)
	}
}

func TestHooksInstallAndExecute(t *testing.T) {
	root := newProject(t, true)
	if r := run(t, root, "", "hooks", "install"); r.code != 0 {
		t.Fatal(r)
	}
	hook := filepath.Join(root, ".git/hooks/pre-push")
	original, err := os.ReadFile(hook) // #nosec G304 -- Reads a synthetic fixture or a file in the test-owned temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".git/hooks/pre-push", "#!/bin/sh\nexit 42\n")
	if r := run(t, root, "", "hooks", "install"); r.code != 1 {
		t.Fatal(r)
	}
	data, _ := os.ReadFile(hook) // #nosec G304 -- Reads a synthetic fixture or a file in the test-owned temporary directory.
	if !strings.Contains(string(data), "exit 42") {
		t.Fatal("existing hook overwritten")
	}
	if r := run(t, root, "", "hooks", "install", "--force"); r.code != 0 {
		t.Fatal(r)
	}
	data, _ = os.ReadFile(hook) // #nosec G304 -- Reads a synthetic fixture or a file in the test-owned temporary directory.
	if !bytes.Equal(data, original) {
		t.Fatal("force did not restore hook")
	}
	// Exercise the installed shell with the real CLI binary, without pushing.
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "naru"), "../../cmd/naru") // #nosec G204 -- Test-controlled executable and arguments exercise the CLI in isolated temporary directories.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	invoke := func(want int) {
		t.Helper()
		cmd := exec.Command(hook, "origin", "unused-local-url")
		cmd.Dir = root
		cmd.Stdin = strings.NewReader("refs/heads/main local refs/heads/main remote\n")
		out, err := cmd.CombinedOutput()
		got := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				got = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if got != want {
			t.Fatalf("hook exit %d want %d: %s", got, want, out)
		}
	}
	invoke(1)
	write(t, root, "src/value.txt", "ok")
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	invoke(0)
	write(t, root, "src/value.txt", "ok, changed")
	invoke(1)
}

// TestPrePushLocalRemote is opt-in because it executes real Git pushes, only
// between temporary local directories. No network remote is used.
func TestPrePushLocalRemote(t *testing.T) {
	if os.Getenv("NARU_TEST_LOCAL_PUSH") != "1" {
		t.Skip("set NARU_TEST_LOCAL_PUSH=1 after approval for temporary local Git pushes")
	}
	root := newProject(t, true)
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-q")
	git(t, root, "add", "src/value.txt")
	git(t, root, "-c", "user.name=Naru Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "naru"), "../../cmd/naru") // #nosec G204 -- Test-controlled executable and arguments exercise the CLI in isolated temporary directories.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if r := run(t, root, "", "hooks", "install"); r.code != 0 {
		t.Fatal(r)
	}
	push := func(ref string, allowed bool) {
		t.Helper()
		cmd := exec.Command("git", "push", remote, "HEAD:refs/heads/"+ref) // #nosec G204 -- Test-controlled executable and arguments exercise the CLI in isolated temporary directories.
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if (err == nil) != allowed {
			t.Fatalf("push %s allowed=%v: %v %s", ref, allowed, err, out)
		}
		probe := exec.Command("git", "--git-dir", remote, "show-ref", "--verify", "refs/heads/"+ref) // #nosec G204 -- Test-controlled executable and arguments exercise the CLI in isolated temporary directories.
		if err := probe.Run(); (err == nil) != allowed {
			t.Fatalf("remote ref %s existence must match allowed=%v: %v", ref, allowed, err)
		}
	}
	push("missing", false)
	write(t, root, "src/value.txt", "ok")
	git(t, root, "add", "src/value.txt")
	git(t, root, "-c", "user.name=Naru Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "passing fixture")
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	push("passing", true)
	write(t, root, ".naru/state/unverified.json", "broken")
	push("unverified", false)
	if r := run(t, root, "", "gate", "stop"); r.code != 0 {
		t.Fatal(r)
	}
	write(t, root, "src/value.txt", "ok edited")
	push("stale", false)
	write(t, root, "README.md", "docs")
	if r := run(t, root, "", "mode", "docs"); r.code != 0 {
		t.Fatal(r)
	}
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	push("narrowed-mode", false)
}
