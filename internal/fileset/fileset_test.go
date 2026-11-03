package fileset

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitInit(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func mustCompute(t *testing.T, root string, inc, exc []string, salt string) Digest {
	t.Helper()
	d, err := Compute(root, inc, exc, salt)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestComputeTracksContentAndSalt(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package a")
	write(t, root, "notes.txt", "x")
	inc := []string{"**/*.go"}

	d1 := mustCompute(t, root, inc, nil, "go test")
	if d1.FileCount != 1 || d1.Source != SourceWalk {
		t.Fatalf("got %+v, want 1 file via walk", d1)
	}
	if d := mustCompute(t, root, inc, nil, "go test"); d.Hash != d1.Hash {
		t.Fatal("hash is not deterministic")
	}

	write(t, root, "notes.txt", "y") // not an input
	if d := mustCompute(t, root, inc, nil, "go test"); d.Hash != d1.Hash {
		t.Fatal("non-input change altered the hash")
	}

	write(t, root, "a.go", "package a // changed")
	d2 := mustCompute(t, root, inc, nil, "go test")
	if d2.Hash == d1.Hash {
		t.Fatal("input change did not alter the hash")
	}
	if d := mustCompute(t, root, inc, nil, "go test -race"); d.Hash == d2.Hash {
		t.Fatal("command change did not alter the hash")
	}
	write(t, root, "sub/b.go", "package b")
	if d := mustCompute(t, root, inc, nil, "go test"); d.Hash == d2.Hash || d.FileCount != 2 {
		t.Fatalf("new input file not picked up: %+v", d)
	}
}

func TestComputeExcludesNaruStateAndConfiguredGlobs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "package a")
	base := mustCompute(t, root, []string{"**"}, []string{"gen/**"}, "")
	write(t, root, ".naru/receipts/test.json", "{}")
	write(t, root, "gen/x.go", "package gen")
	if d := mustCompute(t, root, []string{"**"}, []string{"gen/**"}, ""); d.Hash != base.Hash {
		t.Fatal(".naru/ or excluded files changed the hash")
	}
}

func TestComputeInGitRepoHonoursGitignoreAndUntracked(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	write(t, root, ".gitignore", "build/\n")
	write(t, root, "a.go", "package a")
	d1 := mustCompute(t, root, []string{"**/*.go"}, nil, "")
	if d1.Source != SourceGit || d1.FileCount != 1 {
		t.Fatalf("got %+v, want 1 untracked file via git", d1)
	}
	write(t, root, "build/gen.go", "package gen") // ignored
	if d := mustCompute(t, root, []string{"**/*.go"}, nil, ""); d.Hash != d1.Hash {
		t.Fatal("gitignored file changed the hash")
	}
	if err := os.Remove(filepath.Join(root, "a.go")); err != nil {
		t.Fatal(err)
	}
	if d := mustCompute(t, root, []string{"**/*.go"}, nil, ""); d.Hash == d1.Hash || d.FileCount != 0 {
		t.Fatalf("deleting an input did not change the hash: %+v", d)
	}
}

func TestComputeTracksSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	write(t, root, "real/a.txt", "a")
	write(t, root, "real/b.txt", "b")
	if err := os.Symlink("real/a.txt", filepath.Join(root, "current.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	d1 := mustCompute(t, root, []string{"current.txt"}, nil, "")
	if d1.FileCount != 1 {
		t.Fatalf("symlink input not counted: %+v", d1)
	}
	os.Remove(filepath.Join(root, "current.txt"))
	if err := os.Symlink("real/b.txt", filepath.Join(root, "current.txt")); err != nil {
		t.Fatal(err)
	}
	if d := mustCompute(t, root, []string{"current.txt"}, nil, ""); d.Hash == d1.Hash {
		t.Fatal("retargeting a symlink did not change the hash")
	}
}
