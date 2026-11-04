package fileset

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Source describes how the file list was obtained.
type Source string

const (
	// SourceGit lists tracked and untracked, non-ignored files via git.
	SourceGit Source = "git"
	// SourceWalk walks the directory tree (non-git repositories, or a root
	// that the enclosing repository ignores).
	SourceWalk Source = "walk"
)

// alwaysExcluded never contributes to a hash: naru's own state and VCS metadata
// at any depth (nested repositories and submodules included).
var alwaysExcluded = []string{".naru/**", ".git", ".git/**", "**/.git", "**/.git/**"}

// gitEnvOverrides are cleared so an inherited GIT_DIR (for example when naru
// runs inside a git hook) cannot make git list a different tree or index.
var gitEnvOverrides = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_NAMESPACE"}

func gitCmd(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...) // #nosec G204 -- Fixed git executable and internal argv; no shell interpolation.
	cmd.Dir = root
	var env []string
	for _, kv := range os.Environ() {
		keep := true
		for _, k := range gitEnvOverrides {
			if strings.HasPrefix(kv, k+"=") {
				keep = false
				break
			}
		}
		if keep {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	return cmd
}

// List returns root-relative, slash-separated paths of hashable entries under root.
//
// Inside a git work tree it lists tracked and untracked files that are not
// ignored; directories git reports as single entries (submodules, untracked
// nested repositories) are expanded by walking them. Outside git, or when the
// enclosing repository ignores root itself, it walks the tree. Any other git
// failure is returned, because silently switching listing semantics could hide
// changes.
func List(root string) ([]string, Source, error) {
	inside, err := insideGitWorkTree(root)
	if err != nil {
		return nil, "", err
	}
	if !inside || rootIgnored(root) {
		files, err := walkList(root, "")
		return files, SourceWalk, err
	}
	files, err := gitList(root)
	return files, SourceGit, err
}

func insideGitWorkTree(root string) (bool, error) {
	if _, err := exec.LookPath("git"); err != nil {
		if hasGitAncestor(root) {
			return false, errors.New("this directory is inside a git repository but git is not on PATH")
		}
		return false, nil
	}
	cmd := gitCmd(root, "rev-parse", "--is-inside-work-tree")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)) == "true", nil
	}
	if strings.Contains(stderr.String(), "not a git repository") {
		return false, nil
	}
	return false, fmt.Errorf("git rev-parse failed: %v: %s", err, strings.TrimSpace(stderr.String()))
}

func hasGitAncestor(dir string) bool {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// rootIgnored reports whether the enclosing repository ignores root (for
// example a dotfiles repository at $HOME that ignores project directories).
func rootIgnored(root string) bool {
	return gitCmd(root, "check-ignore", "-q", ".").Run() == nil
}

func gitList(root string) ([]string, error) {
	cmd := gitCmd(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-files failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	seen := map[string]bool{}
	var files []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	for _, p := range strings.Split(out.String(), "\x00") {
		if p == "" {
			continue
		}
		p = strings.TrimSuffix(p, "/")
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue // tracked but deleted: absence is reflected in the hash
		}
		if info.IsDir() {
			// Submodule (gitlink) or untracked nested repository: hash its contents.
			nested, err := walkList(filepath.Join(root, filepath.FromSlash(p)), p+"/")
			if err != nil {
				return nil, err
			}
			for _, n := range nested {
				add(n)
			}
			continue
		}
		if hashable(info.Mode()) {
			add(p)
		}
	}
	sort.Strings(files)
	return files, nil
}

// walkList lists hashable entries under dir, prefixing each relative path.
// Entries named .git are skipped at every depth, and so is .naru at the top level.
func walkList(dir, prefix string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = prefix + filepath.ToSlash(rel)
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if rel == ".naru" {
				return filepath.SkipDir
			}
			return nil
		}
		if hashable(d.Type()) {
			files = append(files, rel)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// hashable reports whether a path contributes to a digest: regular files by
// content, symlinks by their target text (targets are not followed, so a link
// cannot pull files from outside the repository into the hash).
func hashable(m fs.FileMode) bool {
	return m.IsRegular() || m&fs.ModeSymlink != 0
}

// Digest is the content fingerprint of a check's inputs.
type Digest struct {
	Hash      string
	FileCount int
	Source    Source
}

// Compute hashes every listed entry matching include and not matching exclude,
// together with salt (the canonical check definition), so changing either the
// files or the way the check runs invalidates receipts.
func Compute(root string, include, exclude []string, salt string) (Digest, error) {
	inc, err := NewMatcher(include)
	if err != nil {
		return Digest{}, fmt.Errorf("inputs: %w", err)
	}
	exc, err := NewMatcher(append(append([]string{}, alwaysExcluded...), exclude...))
	if err != nil {
		return Digest{}, fmt.Errorf("exclude: %w", err)
	}
	files, src, err := List(root)
	if err != nil {
		return Digest{}, err
	}
	h := sha256.New()
	writeField(h, "naru-inputs-v2")
	writeField(h, salt)
	n := 0
	for _, f := range files {
		if !inc.Match(f) || exc.Match(f) {
			continue
		}
		sum, err := entrySum(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return Digest{}, err
		}
		writeField(h, f)
		writeField(h, sum)
		n++
	}
	return Digest{Hash: "sha256:" + hex.EncodeToString(h.Sum(nil)), FileCount: n, Source: src}, nil
}

// writeField length-prefixes each value so no two different input sets can
// produce the same byte stream.
func writeField(w hash.Hash, s string) {
	_, _ = fmt.Fprintf(w, "%d:%s", len(s), s) // hash.Hash.Write never returns an error.
}

// entrySum fingerprints one entry: symlinks by target, regular files by content
// plus the executable bit (so `chmod -x gradlew` is a change).
func entrySum(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		return "symlink:" + filepath.ToSlash(target), nil
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return "", err
	}
	if info.Mode().Perm()&0o111 != 0 {
		return "x:" + sum, nil
	}
	return "f:" + sum, nil
}

func fileSHA256(path string) (sum string, retErr error) {
	f, err := os.Open(path) // #nosec G304 -- Hashes selected local project inputs; this API is not a filesystem confinement boundary.
	if err != nil {
		return "", err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// GitHead returns the current commit id, or "" outside git / before the first commit.
func GitHead(root string) string {
	out, err := gitCmd(root, "rev-parse", "--verify", "-q", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
