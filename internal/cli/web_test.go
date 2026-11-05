package cli

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/surface"
)

func webPython(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Web pack requires POSIX process groups and flock; Windows runtime unsupported")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 required to verify Web adapter fixtures; no build or download is needed")
	}
	return python
}

func webFixtures(t *testing.T, groups ...string) {
	t.Helper()
	python := webPython(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := append([]string{"-B", "../../packs/web/test_extract.py"}, groups...)
	// #nosec G204 -- Fixed repository-owned fixture suite and named unittest groups; no npm downloads.
	output, err := exec.CommandContext(ctx, python, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("Web adapter fixtures: %v\n%s", err, output)
	}
}

func TestWebFixtures(t *testing.T) {
	webFixtures(t, "SurfaceTests")
	webRuntimeFixtures(t)
	for _, kind := range []string{"routes", "dependencies", "env"} {
		data, err := os.ReadFile("testdata/web/" + kind + ".json") // #nosec G304 -- Fixed public synthetic fixture names.
		if err != nil {
			t.Fatal(err)
		}
		if _, err := surface.Build(surface.Request{Schema: 1, SurfaceID: "web." + kind, ExtractorVersion: "web-v1", InputDigest: strings.Repeat("a", 64)}, data, nil); err != nil {
			t.Fatalf("%s normalized fixture violates surface contract: %v", kind, err)
		}
	}
}

// This local link verifies the runtime without npm or downloads. Actual npm
// offline workspace preparation is independently required by the live smoke.
func webRuntimeFixtures(t *testing.T) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required to verify Web runtime fixtures; no npm install is needed")
	}
	root := webProject(t)
	sample := filepath.Join(root, "examples/web")
	modules := filepath.Join(sample, "node_modules")
	if err := os.Mkdir(modules, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sample, "packages/message"), filepath.Join(modules, "naru-web-message")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Direct node:test module execution avoids a separate test-runner child.
	// #nosec G204 -- Fixed public runtime test in an isolated synthetic sample.
	cmd := exec.CommandContext(ctx, node, "test/runtime.test.mjs")
	cmd.Dir = sample
	cmd.Env = []string{"PATH=" + filepath.Dir(node), "HOME=" + t.TempDir(), "TMPDIR=" + t.TempDir(), "LANG=C.UTF-8"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Web runtime fixtures: %v\n%s", err, output)
	}
}

// webProject copies repository fixtures without private environment/config files
// or npm outputs from the separate real smoke.
func webProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"packs/web", "examples/web"} {
		err := filepath.WalkDir(filepath.Join("../..", dir), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "node_modules", "dist", "build", ".naru", "__pycache__":
					return filepath.SkipDir
				}
				return nil
			}
			if (strings.HasPrefix(entry.Name(), ".env") && entry.Name() != ".env.example") || entry.Name() == ".npmrc" {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				t.Fatalf("unexpected sample symlink: %s", path)
			}
			data, err := os.ReadFile(path) // #nosec G304 G122 -- Fixed repository fixture trees; tests do not mutate sources during this walk. Symlinks are rejected above; Go 1.23 has no os.Root API.
			if err != nil {
				return err
			}
			rel, err := filepath.Rel("../..", path)
			if err != nil {
				return err
			}
			write(t, root, rel, string(data))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	write(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: code\npacks: [packs/web/pack.yaml]\nmodes: {code: [web.contracts]}\n")
	return root
}

func webSeedBaseline(t *testing.T, root, name string) []byte {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Surfaces[name]
	d, err := fileset.Compute(root, s.Inputs, cfg.Exclude, cfg.SurfaceFingerprint(name))
	if err != nil || d.FileCount == 0 {
		t.Fatalf("sample inputs unavailable: %+v %v", d, err)
	}
	data, err := os.ReadFile("testdata/web/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := surface.Build(surface.Request{Schema: 1, SurfaceID: name, ExtractorVersion: s.ExtractorVersion, InputDigest: strings.TrimPrefix(d.Hash, "sha256:")}, data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := (baseline.Store{Root: root}).Put(baseline.State{Schema: 1, SurfaceID: name, Phase: "ready", Snapshot: snapshot}); err != nil {
		t.Fatal(err)
	}
	preview, err := baseline.Diff(root, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baseline.Approve(context.Background(), root, name, *preview, "fixture reviewer", "synthetic approved baseline"); err != nil {
		t.Fatal(err)
	}
	approved, err := os.ReadFile(filepath.Join(root, ".naru/baselines", name+".json")) // #nosec G304 -- Fixed test-owned baseline path.
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

func TestWebInvalidOutput(t *testing.T) {
	t.Run("adapter_negative_fixtures", func(t *testing.T) {
		webFixtures(t, "ValidationTests", "BuildTests")
	})
	python := webPython(t)
	for _, scenario := range []string{"missing_declared_tool", "present_tools_failed_build"} {
		t.Run(scenario, func(t *testing.T) {
			root := webProject(t)
			bin := t.TempDir()
			for _, tool := range []string{"node", "npm"} {
				path := filepath.Join(bin, tool)
				if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o600); err != nil { // #nosec G703 -- Fixed fake-tool names in a test-owned bin directory.
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- Owner-only executable fixture needed for PATH prerequisite discovery.
					t.Fatal(err)
				}
			}
			for name, target := range map[string]string{"python3": python, "sh": "/bin/sh"} {
				if err := os.Symlink(target, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)
			webSeedCheck(t, root)
			if got := run(t, root, "", "status"); got.code != 0 {
				t.Fatalf("synthetic prior success not accepted before failure: %+v", got)
			}
			if scenario == "missing_declared_tool" {
				t.Setenv("PATH", t.TempDir())
			}
			approved := webSeedBaseline(t, root, "web.routes")
			if got := run(t, root, "", "check", "web.contracts"); got.code != 1 {
				t.Fatalf("unavailable/failing build passed: %+v", got)
			}
			state, err := receipt.NewStateStore(root).Current("web.contracts")
			if err != nil || state == nil || state.Execution.Result == nil {
				t.Fatalf("missing check result: %+v %v", state, err)
			}
			if scenario == "missing_declared_tool" && *state.Execution.Result != receipt.ResultNotRun {
				t.Fatalf("missing tool was not not-run: %+v", state)
			}
			if scenario == "present_tools_failed_build" && *state.Execution.Result != receipt.ResultFail {
				t.Fatalf("present tool build failure misclassified: %+v", state)
			}
			if got := run(t, root, "", "status"); got.code != 1 {
				t.Fatalf("prior successful check reused after failed attempt: %+v", got)
			}
			if got := run(t, root, "", "baseline", "extract", "web.routes"); got.code != 1 {
				t.Fatalf("failed extraction accepted: %+v", got)
			}
			candidate, err := (baseline.Store{Root: root}).Current("web.routes")
			if err != nil || candidate == nil || candidate.Phase != "failed" || candidate.Snapshot != nil {
				t.Fatalf("old candidate retained authority: %+v %v", candidate, err)
			}
			if got := run(t, root, "", "baseline", "diff", "web.routes"); got.code != 1 {
				t.Fatalf("old candidate reused: %+v", got)
			}
			after, err := os.ReadFile(filepath.Join(root, ".naru/baselines/web.routes.json")) // #nosec G304 -- Fixed test-owned baseline path.
			if err != nil || !bytes.Equal(approved, after) {
				t.Fatalf("failure mutated approved baseline: %v", err)
			}
		})
	}
}

// webSeedCheck publishes synthetic execution evidence, not a claim that the Web build ran.
// The test proves a subsequent unavailable/failed execution replaces its authority.
func webSeedCheck(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Checks["web.contracts"]
	d, err := fileset.Compute(root, c.Inputs, cfg.Exclude, cfg.Fingerprint("web.contracts"))
	if err != nil || d.FileCount == 0 {
		t.Fatalf("missing fixture inputs: %+v %v", d, err)
	}
	now := time.Now().UTC()
	runID := strings.Repeat("a", 32)
	digest := receipt.InputDigest{Hash: d.Hash, FileCount: d.FileCount, FileSource: string(d.Source)}
	r := receipt.Record{Schema: receipt.StateSchema, Check: "web.contracts", RunID: runID, Phase: receipt.PhaseRunning, StartedAt: now,
		Provenance: receipt.Provenance{Kind: receipt.EvidenceLocal},
		Execution:  receipt.Execution{Command: c.Run, Workdir: ".", TimeoutMS: c.TimeoutDuration().Milliseconds(), LogPath: ".naru/logs/web.contracts-" + runID + ".log", NaruVersion: "dev"},
		Inputs:     receipt.Inputs{Policy: receipt.InputPolicy, Before: digest, Validity: receipt.InputsPending}}
	store := receipt.NewStateStore(root)
	if err := store.Begin(r); err != nil {
		t.Fatal(err)
	}
	result, code, duration := receipt.ResultPass, 0, int64(0)
	r.Phase, r.FinishedAt = receipt.PhaseTerminal, &now
	r.Execution.Result, r.Execution.ExitCode, r.Execution.DurationMS = &result, &code, &duration
	r.Inputs.After, r.Inputs.Validity = &digest, receipt.InputsMatched
	if err := store.Put(r); err != nil {
		t.Fatal(err)
	}
}
