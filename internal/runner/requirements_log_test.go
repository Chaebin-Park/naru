package runner

import (
	"errors"
	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"os"
	"path/filepath"
	"testing"
)

func TestUnavailableLogFailureCannotRestorePriorPass(t *testing.T) {
	root, cfg := stateProject(t, "true")
	if _, err := stateRun(root, cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	cfg = requireConfig(t, root, "{tools: [naru-nonexistent-fixture-tool]}")
	_, err := runWithLog(root, cfg, "c", Options{}, func(path string) (logSink, error) {
		f, e := os.Create(path) // #nosec G304 -- Generated log path in this test-owned project.
		if e != nil {
			return nil, e
		}
		return &faultyLog{File: f, failure: "header"}, nil
	})
	if !errors.Is(err, errLog) {
		t.Fatalf("missing log error: %v", err)
	}
	current, err := receipt.NewStateStore(root).Current("c")
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.Execution.Result == nil || *current.Execution.Result != receipt.ResultNotRun {
		t.Fatal("log failure retained prior pass")
	}
}

func requireConfig(t *testing.T, root, requirement string) *config.Config {
	t.Helper()
	p := filepath.Join(root, ".naru/config.yaml")
	b, err := os.ReadFile(p) // #nosec G304 -- Config fixture in a test-owned temporary project.
	if err != nil {
		t.Fatal(err)
	}
	stateWrite(t, root, ".naru/config.yaml", string(b)+"    requires: "+requirement+"\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
