package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := []string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "protocol.file.allow=always", "-c", "init.defaultBranch=main"}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

const anyInputs = `version: 1
default_mode: code
modes: {code: [t]}
checks:
  t:
    run: "true"
    inputs: ["**"]
`

func passThenEdit(t *testing.T, root, edit string) {
	t.Helper()
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatalf("check: %+v", r)
	}
	write(t, root, edit, "changed\n")
	if r := run(t, root, "", "status"); r.code != 1 || !strings.Contains(r.stdout, "stale") {
		t.Fatalf("edit to %s was not detected: %+v", edit, r)
	}
}

// Review finding 1: inputs that match nothing must never pass.
func TestEmptyInputsNeverPass(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".naru/config.yaml", strings.Replace(anyInputs, `["**"]`, `["src/**"]`, 1))
	write(t, root, "lib/a.ts", "x")
	if r := run(t, root, "", "check"); r.code != 64 || !strings.Contains(r.stderr, "match no files") {
		t.Fatalf("check with empty inputs: %+v", r)
	}
	r := run(t, root, stopPayload(root), "gate", "stop")
	if r.code != 2 || !strings.Contains(r.stderr, "match no files") {
		t.Fatalf("gate with empty inputs: %+v", r)
	}
}

// Review finding 2: edits inside an untracked nested repository and a submodule.
func TestNestedRepositoriesAreHashed(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	write(t, root, ".naru/config.yaml", anyInputs)
	write(t, root, "nested/x.txt", "a")
	git(t, filepath.Join(root, "nested"), "init", "-q")
	passThenEdit(t, root, "nested/x.txt")

	lib := t.TempDir()
	git(t, lib, "init", "-q")
	write(t, lib, "l.go", "package l")
	git(t, lib, "add", ".")
	git(t, lib, "commit", "-qm", "init")
	git(t, root, "submodule", "add", "-q", lib, "lib")
	passThenEdit(t, root, "lib/l.go")
}

// Review finding 3: a naru root inside a directory the enclosing repo ignores.
func TestRootIgnoredByEnclosingRepository(t *testing.T) {
	outer := t.TempDir()
	git(t, outer, "init", "-q")
	write(t, outer, ".gitignore", "proj/\n")
	root := filepath.Join(outer, "proj")
	write(t, root, ".naru/config.yaml", anyInputs)
	write(t, root, "src/a.txt", "a")
	passThenEdit(t, root, "src/a.txt")
}

// Review finding 4: an empty mode would let any change through.
func TestEmptyModeRejectedAndNarrowedModeReported(t *testing.T) {
	root := newProject(t, false)
	cfg := strings.Replace(projectConfig, "  docs: [has-readme]\n", "  docs: [has-readme]\n  none: []\n", 1)
	write(t, root, ".naru/config.yaml", cfg)
	if r := run(t, root, "", "status"); r.code != 64 || !strings.Contains(r.stderr, `mode "none" has no checks`) {
		t.Fatalf("empty mode accepted: %+v", r)
	}
	write(t, root, ".naru/config.yaml", projectConfig)
	write(t, root, "README.md", "# x")
	run(t, root, "", "mode", "docs")
	run(t, root, "", "check")
	r := run(t, root, stopPayload(root), "gate", "stop")
	if r.code != 0 || !strings.Contains(r.stderr, `done for mode "docs" (selected via stored; default_mode is "code")`) {
		t.Fatalf("narrowed mode not surfaced: %+v", r)
	}
}

// Review finding 5: changing how a check runs invalidates its receipt.
func TestCheckDefinitionChangeIsStale(t *testing.T) {
	root := t.TempDir()
	write(t, root, "good/x", "x")
	write(t, root, ".naru/config.yaml", anyInputs+"    workdir: good\n")
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatalf("check: %+v", r)
	}
	write(t, root, ".naru/config.yaml", anyInputs+"    workdir: bad\n")
	if r := run(t, root, "", "status"); r.code != 1 || !strings.Contains(r.stdout, "stale") {
		t.Fatalf("workdir change not detected: %+v", r)
	}
	write(t, root, ".naru/config.yaml", anyInputs+"    workdir: good\nexclude: [\"good/**\"]\n")
	if r := run(t, root, "", "status"); r.code != 1 {
		t.Fatalf("exclude change not detected: %+v", r)
	}
}

// Review finding 10: nested .git internals never affect a walk-mode hash.
func TestWalkIgnoresNestedGitDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".naru/config.yaml", anyInputs)
	write(t, root, "a.txt", "a")
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatalf("check: %+v", r)
	}
	write(t, root, "sub/.git/HEAD", "ref: refs/heads/main\n")
	write(t, root, "wt/.git", "gitdir: /elsewhere\n") // a .git file, as in worktrees and submodules
	if r := run(t, root, "", "status"); r.code != 0 {
		t.Fatalf("nested .git changed the hash: %+v", r)
	}
}

func TestExecutableBitIsAnInput(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".naru/config.yaml", anyInputs)
	write(t, root, "gradlew", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(root, "gradlew"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, root, "", "check")
	if err := os.Chmod(filepath.Join(root, "gradlew"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := run(t, root, "", "status"); r.code != 1 {
		t.Fatalf("chmod -x not detected: %+v", r)
	}
}

// Review finding 6/7: project dir from Claude Code wins over the payload cwd,
// and a human can switch the gate off.
func TestGateProjectDirAndEscapeHatch(t *testing.T) {
	root := newProject(t, false)
	elsewhere := t.TempDir()
	var out, errb strings.Builder
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	code := Run([]string{"gate", "stop"}, Env{Stdin: strings.NewReader(stopPayload(elsewhere)), Stdout: &out, Stderr: &errb, Dir: elsewhere})
	if code != 2 {
		t.Fatalf("cd out of the project escaped the gate: code=%d %s", code, errb.String())
	}
	t.Setenv("NARU_GATE", "off")
	errb.Reset()
	code = Run([]string{"gate", "stop"}, Env{Stdin: strings.NewReader(stopPayload(root)), Stdout: &out, Stderr: &errb, Dir: root})
	if code != 0 || !strings.Contains(errb.String(), "gate disabled") {
		t.Fatalf("NARU_GATE=off: code=%d %s", code, errb.String())
	}
}
