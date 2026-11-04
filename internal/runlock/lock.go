// Package runlock serializes executions of one check within a working copy.
package runlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Chaebin-Park/naru/internal/config"
)

var ErrBusy = errors.New("check is already running")

// Lock owns an OS lock on a permanent file. Closing never unlinks that file.
type Lock struct {
	file *os.File
	Root string
}

// Acquire canonicalizes root and takes a nonblocking exclusive lock.
func Acquire(root, check string) (*Lock, error) {
	if !config.NamePattern.MatchString(check) {
		return nil, errors.New("invalid check name")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, ".naru", "state", "locks")
	for _, p := range []string{filepath.Join(root, ".naru"), filepath.Join(root, ".naru/state"), dir} {
		info, err := os.Lstat(p) // #nosec G703 -- Caller selects the local repository root; this is not an external path confinement API.
		if err == nil && !info.IsDir() {
			return nil, fmt.Errorf("lock directory is not a plain directory: %s", p)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil { // #nosec G703 -- Caller selects the local repository root; this is not an external path confinement API.
		return nil, err
	}
	path := filepath.Join(dir, check+".lock")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() { // #nosec G703 -- Caller selects the local repository root; this is not an external path confinement API.
		return nil, errors.New("lock is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 G703 -- Validated check name in canonical working-copy lock directory.
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return &Lock{file: f, Root: root}, nil
}
func (l *Lock) Close() error { return l.file.Close() }
