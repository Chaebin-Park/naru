// Package mode resolves which mode's checks are required right now.
package mode

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Chaebin-Park/naru/internal/config"
)

// EnvVar overrides the stored mode for one process.
const EnvVar = "NARU_MODE"

func statePath(root string) string {
	return filepath.Join(root, config.Dir, "state", "mode")
}

// Resolve picks the mode: explicit flag > $NARU_MODE > stored mode > default_mode.
// It also reports where the choice came from.
func Resolve(root string, cfg *config.Config, flag string) (name, source string) {
	if flag != "" {
		return flag, "flag"
	}
	if v := os.Getenv(EnvVar); v != "" {
		return v, "env"
	}
	if v, err := Stored(root); err == nil && v != "" {
		return v, "stored"
	}
	return cfg.DefaultMode, "default"
}

// Stored returns the mode saved by `naru mode <name>`, or "" when none is set.
func Stored(root string) (string, error) {
	data, err := os.ReadFile(statePath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(data)), err
}

// Store saves the mode for subsequent commands in this working copy.
func Store(root, name string) error {
	p := statePath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(name+"\n"), 0o600)
}

// Clear removes the stored mode so default_mode applies again.
func Clear(root string) error {
	err := os.Remove(statePath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
