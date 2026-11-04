// Package hooks installs an opt-in Git pre-push hook.
package hooks

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const script = "#!/bin/sh\n# Installed by naru hooks install.\nexec naru gate pre-push\n"

// Install preserves existing hooks unless force is explicit. Custom hook
// managers remain responsible for their own files, even with force.
func Install(root string, force bool) (path string, retErr error) {
	// Installing into an inherited repository context could change another
	// working copy. Require an ordinary invocation for this mutating command.
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR"} {
		if os.Getenv(key) != "" {
			return "", fmt.Errorf("unset %s before installing hooks", key)
		}
	}
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...) // #nosec G204 -- Fixed git executable and internal argv query the explicitly selected repository.
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("cannot find Git working tree: %s: %w", top, err)
	}
	a, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	b, err := filepath.EvalSymlinks(top)
	if err != nil || a != b {
		return "", fmt.Errorf("naru project must be at the Git working tree root")
	}
	custom, err := git("config", "--get", "core.hooksPath")
	if err == nil {
		return "", fmt.Errorf("core.hooksPath is configured (%q); add `naru gate pre-push` to your hook manager instead", custom)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		return "", fmt.Errorf("cannot inspect core.hooksPath: %s: %w", custom, err)
	}
	p, err := git("rev-parse", "--git-path", "hooks/pre-push")
	if err != nil {
		return "", fmt.Errorf("cannot find hooks directory: %s: %w", p, err)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	if info, err := os.Lstat(p); err == nil {
		if !force {
			return "", fmt.Errorf("hook already exists at %s; integrate manually or use --force to replace it", p)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("refusing to replace a non-regular hook at %s", p)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { // #nosec G301 -- Git hook directories are intentionally readable and searchable by repository users.
		return "", err
	}
	if !force {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755) // #nosec G302 G304 -- Git must execute the installed hook; script contains no secrets. Path comes from git for the verified working tree; exclusive creation refuses existing entries.
		if err != nil {
			return "", err
		}
		_, writeErr := f.WriteString(script)
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return "", err
		}
		return p, nil
	}
	// Rename a complete executable file so force never truncates the old hook.
	f, err := os.CreateTemp(filepath.Dir(p), ".naru-pre-push-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err := os.Remove(f.Name()); !errors.Is(err, os.ErrNotExist) {
			retErr = errors.Join(retErr, err)
		}
	}()
	_, writeErr := f.WriteString(script)
	chmodErr := f.Chmod(0o755)
	closeErr := f.Close()
	if err := errors.Join(writeErr, chmodErr, closeErr); err != nil {
		return "", err
	}
	if err := os.Rename(f.Name(), p); err != nil {
		return "", err
	}
	return p, nil
}
