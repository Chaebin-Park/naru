package gate

import (
	"bytes"
	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/runner"
	"os"
	"path/filepath"
	"testing"
)

func TestBreakerBlocksWhenUnverifiedCannotBeSaved(t *testing.T) {
	root := t.TempDir()
	// A directory at the record path forces a deterministic write failure.
	if err := os.MkdirAll(unverifiedPath(root), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if got := block(root, StopInput{}, 1, "same", "pending", &stdout, &stderr); got != Block {
		t.Fatal(got)
	}
	if got := block(root, StopInput{StopHookActive: true}, 1, "same", "pending", &stdout, &stderr); got != Block {
		t.Fatalf("allowed completion without saving NOT VERIFIED: exit %d", got)
	}
	if bytes.Contains(stdout.Bytes(), []byte("let the agent stop")) {
		t.Fatal("reported an unsafe allowance")
	}
}

func TestVerifiedStopBlocksWhenMarkerCannotBeRemoved(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(unverifiedPath(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unverifiedPath(root), "child"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := "version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks: {c: {run: 'true', inputs: [in.txt]}}\n"
	if err := os.WriteFile(filepath.Join(root, ".naru", "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "in.txt"), []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(root, parsed, "c", runner.Options{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(DisableEnv, "")
	t.Setenv(ProjectDirEnv, root)
	var stdout, stderr bytes.Buffer
	if got := Stop(bytes.NewBufferString("{}"), &stdout, &stderr, root); got != Block {
		t.Fatalf("exit %d: %s", got, stderr.String())
	}
}
