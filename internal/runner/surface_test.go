package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

func surfaceConfig(t *testing.T, root, command, extra string) {
	t.Helper()
	stateWrite(t, root, ".naru/config.yaml", fmt.Sprintf("version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks: {c: {run: 'true', inputs: [in.txt]}}\nsurfaces:\n  api:\n    run: %q\n    inputs: [in.txt, surface.json]\n    extractor_version: v1\n    timeout: 2s\n%s", command, extra))
}
func surfaceProject(t *testing.T) string {
	t.Helper()
	root, _ := stateProject(t, "true")
	stateWrite(t, root, "surface.json", `{"schema":1,"items":[{"id":"route","value":{"method":"GET"}}]}`)
	surfaceConfig(t, root, "cat surface.json", "")
	return root
}
func TestSurfaceExtractionLifecycle(t *testing.T) {
	root := surfaceProject(t)
	first, err := Extract(root, "api", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	next, err := Extract(root, "api", context.Background())
	if err != nil || next.Digest != first.Digest {
		t.Fatalf("repeat %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".naru/baselines/api.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("extract wrote approved baseline")
	}
	for _, tc := range []struct{ name, command, extra, reason string }{
		{"failed", "exit 1", "", "extractor_failed"},
		{"empty stdout", "true", "", "output_invalid"},
		{"input changed", "printf changed >> in.txt; cat surface.json", "", "input_changed"},
		{"missing tool", "echo invoked > invoked", "    requires: {tools: [naru-missing-fixture-tool]}\n", "missing_tool"},
		{"output limit", "while :; do printf '" + strings.Repeat("x", 128) + "'; done", "", "output_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			surfaceConfig(t, root, tc.command, tc.extra)
			if s, err := Extract(root, "api", context.Background()); s != nil || err == nil {
				t.Fatal("invalid extraction accepted")
			}
			state, err := (baseline.Store{Root: root}).Current("api")
			if err != nil || state.Phase != "failed" || state.Reason != tc.reason || state.Snapshot != nil {
				t.Fatalf("state %+v %v", state, err)
			}
			if _, err := baseline.Diff(root, "api"); !errors.Is(err, baseline.ErrNotReady) {
				t.Fatalf("failed candidate reused: %v", err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "invoked")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unavailable command invoked")
	}
	surfaceConfig(t, root, "echo diagnostic >&2; cat surface.json", "")
	if _, err := Extract(root, "api", context.Background()); err != nil {
		t.Fatal(err)
	}
	lock, err := baseline.Acquire(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := Extract(root, "api", context.Background())
	closeErr := lock.Close()
	if !errors.Is(runErr, runlock.ErrBusy) || closeErr != nil {
		t.Fatalf("duplicate %v %v", runErr, closeErr)
	}
}
func TestSurfaceCancellationAndRecovery(t *testing.T) {
	root := surfaceProject(t)
	surfaceConfig(t, root, "printf started > started; sleep 30", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := Extract(root, "api", ctx); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("extractor did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupted) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop extractor")
	}
	store := baseline.Store{Root: root}
	state, err := store.Current("api")
	if err != nil || state.Phase != "failed" || state.Reason != "cancelled" {
		t.Fatalf("cancel %+v %v", state, err)
	}
	if err := store.Put(baseline.State{Schema: 1, SurfaceID: "api", Phase: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(root, "api", context.Background()); !errors.Is(err, ErrRecovery) {
		t.Fatalf("orphan restarted: %v", err)
	}
}

func TestSurfaceWorkdirRejectsSymlink(t *testing.T) {
	root := surfaceProject(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	surfaceConfig(t, root, "echo escaped > marker", "    workdir: outside\n")
	if _, err := Extract(root, "api", context.Background()); err == nil {
		t.Fatal("symlink workdir accepted")
	}
	state, err := (baseline.Store{Root: root}).Current("api")
	if err != nil || state.Reason != "workdir_invalid" {
		t.Fatalf("state %+v %v", state, err)
	}
}
