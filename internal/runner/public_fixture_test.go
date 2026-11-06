package runner_test

import (
	"encoding/json"
	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/runner"
	"os"
	"path/filepath"
	"testing"
)

func stateProject(t *testing.T, command string) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".naru"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateWrite(t, root, "in.txt", "original")
	return root, stateConfig(t, root, command)
}
func stateConfig(t *testing.T, root, command string) *config.Config {
	t.Helper()
	quoted, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	stateWrite(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks:\n  c:\n    inputs: [in.txt]\n    timeout: 10s\n    run: "+string(quoted)+"\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func stateWrite(t *testing.T, root, path, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o600); err != nil { // #nosec G703 -- Test-owned project root and fixture-relative paths; no external filesystem input.
		t.Fatal(err)
	}
}

func stateRun(root string, cfg *config.Config, opt runner.Options) (runner.Outcome, error) {
	return runner.Run(root, cfg, "c", opt)
}
