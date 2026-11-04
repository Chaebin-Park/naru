package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/gate"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/status"
)

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
func assertEnvironmentState(t *testing.T, root string, cfg *config.Config, want status.State) {
	t.Helper()
	rep, err := status.Evaluate(root, cfg, "m")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Satisfied != (want == status.Pass) || rep.Checks[0].State != want {
		t.Fatalf("%+v", rep)
	}
	var out, errout bytes.Buffer
	t.Setenv("NARU_GATE", "")
	t.Setenv("NARU_MODE", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	stop := gate.Stop(strings.NewReader("{}"), &out, &errout, root)
	push := gate.PrePush(root, &errout)
	if want == status.Pass {
		if stop != 0 || push != 0 {
			t.Fatalf("gate %d %d: %s", stop, push, errout.String())
		}
	} else if stop != 2 || push != 1 {
		t.Fatalf("gate bypass: %d %d", stop, push)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if want == status.NotRun && !bytes.Contains(b, []byte(`"state":"not-run"`)) {
		t.Fatalf("report: %s", b)
	}
}
func TestUnavailableEnvironmentLifecycle(t *testing.T) {
	root, _ := stateProject(t, "echo run >> starts")
	cfg := requireConfig(t, root, "{tools: [naru-fixture-tool]}")
	tools := t.TempDir()
	tool := filepath.Join(tools, "naru-fixture-tool")
	install := func() {
		t.Helper()
		if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	} // #nosec G306 -- Executable fixture for PATH discovery; the check must not invoke this tool.
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	install()
	first, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertEnvironmentState(t, root, cfg, status.Pass)
	if err = os.Remove(tool); err != nil {
		t.Fatal(err)
	}
	assertEnvironmentState(t, root, cfg, status.NotRun)
	missing, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec := missing.Record
	if *rec.Execution.Result != receipt.ResultNotRun || rec.Execution.ExitCode != nil || rec.Inputs.Before != (receipt.InputDigest{}) || rec.Inputs.Validity != receipt.InputsUnobserved || rec.Execution.Unavailable.Subject != "naru-fixture-tool" {
		t.Fatalf("%+v", rec)
	}
	current, err := receipt.NewStateStore(root).Current("c")
	if err != nil || current.RunID != rec.RunID {
		t.Fatalf("%+v %v", current, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "starts")) // #nosec G304 -- Test-owned execution counter.
	if err != nil || string(data) != "run\n" {
		t.Fatalf("executed unavailable command: %s %v", data, err)
	}
	archived, err := os.ReadFile(filepath.Join(root, ".naru/state/history/c", first.Record.RunID+".json")) // #nosec G304 -- Test-owned immutable history fixture.
	if err != nil {
		t.Fatal(err)
	}
	old, err := receipt.DecodeRecord(archived, "c")
	if err != nil || *old.Execution.Result != receipt.ResultPass {
		t.Fatalf("archive: %+v %v", old, err)
	}
	install()
	assertEnvironmentState(t, root, cfg, status.NotRun)
	if _, err = stateRun(root, cfg, Options{}); err != nil {
		t.Fatal(err)
	}
	assertEnvironmentState(t, root, cfg, status.Pass)
	data, err = os.ReadFile(filepath.Join(root, "starts")) // #nosec G304 -- Test-owned execution counter.
	if err != nil || string(data) != "run\nrun\n" {
		t.Fatalf("rerun: %s %v", data, err)
	}
	// Changing requirements invalidates previous success even when the tool is present.
	cfg = stateConfig(t, root, "echo run >> starts")
	assertEnvironmentState(t, root, cfg, status.Stale)
}
func TestUnsupportedPlatformDoesNotRun(t *testing.T) {
	root, _ := stateProject(t, "echo started > started")
	unsupported := "windows"
	if runtime.GOOS == unsupported {
		unsupported = "linux"
	}
	cfg := requireConfig(t, root, "{os: ["+unsupported+"]}")
	out, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if *out.Record.Execution.Result != receipt.ResultNotRun || out.Record.Execution.Unavailable.Reason != "unsupported_platform" {
		t.Fatalf("%+v", out.Record)
	}
	if _, err = os.Stat(filepath.Join(root, "started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("command started")
	}
	assertEnvironmentState(t, root, cfg, status.NotRun)
}
func TestMissingEnvironmentPrecedesEmptyInputs(t *testing.T) {
	root, _ := stateProject(t, "true")
	cfg := requireConfig(t, root, "{tools: [naru-nonexistent-fixture-tool]}")
	if err := os.Remove(filepath.Join(root, "in.txt")); err != nil {
		t.Fatal(err)
	}
	out, err := stateRun(root, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if *out.Record.Execution.Result != receipt.ResultNotRun {
		t.Fatal("missing environment misreported as empty inputs")
	}
}

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
