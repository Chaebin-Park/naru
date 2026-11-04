package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- Test-controlled executable and arguments exercise the CLI in isolated temporary directories.
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestInstallWorktree(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "-c", "user.name=Naru Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "test")
	worktree := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "--detach", worktree)
	p, err := Install(worktree, false)
	if err != nil {
		t.Fatal(err)
	}
	want := git(t, root, "rev-parse", "--git-path", "hooks/pre-push")
	if !filepath.IsAbs(want) {
		want = filepath.Join(root, want)
	}
	a, _ := filepath.EvalSymlinks(p)
	b, _ := filepath.EvalSymlinks(want)
	if a != b {
		t.Fatalf("worktree hook %s, want %s", p, want)
	}
}

func TestInstallProtectsManagedAndSpecialHooks(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q")
	t.Run("inherited repository", func(t *testing.T) {
		t.Setenv("GIT_COMMON_DIR", t.TempDir())
		if _, err := Install(root, false); err == nil || !strings.Contains(err.Error(), "unset GIT_COMMON_DIR") {
			t.Fatalf("inherited repository accepted: %v", err)
		}
	})
	t.Run("custom hooks path", func(t *testing.T) {
		// Command-scoped configuration: no repository settings are changed.
		t.Setenv("GIT_CONFIG_COUNT", "1")
		t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
		t.Setenv("GIT_CONFIG_VALUE_0", ".husky/_")
		for _, force := range []bool{false, true} {
			if _, err := Install(root, force); err == nil || !strings.Contains(err.Error(), "hook manager") {
				t.Fatalf("custom hooks: %v", err)
			}
		}
	})
	target := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(root, ".git/hooks/pre-push")
	if err := os.Symlink(target, hook); err != nil {
		t.Skip(err)
	}
	if _, err := Install(root, true); err == nil {
		t.Fatal("replaced symlink")
	}
	data, _ := os.ReadFile(target) // #nosec G304 -- Reads a synthetic fixture or a file in the test-owned temporary directory.
	if string(data) != "preserve" {
		t.Fatal("modified symlink target")
	}
	if _, err := Install(t.TempDir(), false); err == nil {
		t.Fatal("installed outside git")
	}
	sub := filepath.Join(root, "nested")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(sub, false); err == nil {
		t.Fatal("installed for nested project")
	}
}
