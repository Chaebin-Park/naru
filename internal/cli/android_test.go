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

func androidPython(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Android pack requires POSIX process groups and flock; Windows runtime unsupported")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 required to verify Android adapter fixtures; no build or download is needed")
	}
	return python
}

func androidFixtures(t *testing.T, groups ...string) {
	t.Helper()
	python := androidPython(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := append([]string{"-B", "../../packs/android/test_extract.py"}, groups...)
	// #nosec G204 -- Fixed repository-owned fixture suite and named unittest groups; no Gradle downloads.
	output, err := exec.CommandContext(ctx, python, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("Android adapter fixtures: %v\n%s", err, output)
	}
}

func TestAndroidFixtures(t *testing.T) {
	androidFixtures(t, "SurfaceTests")
	for _, kind := range []string{"manifest", "dependencies"} {
		data, err := os.ReadFile("testdata/android/" + kind + ".json") // #nosec G304 -- Fixed public synthetic fixture names.
		if err != nil {
			t.Fatal(err)
		}
		if _, err := surface.Build(surface.Request{Schema: 1, SurfaceID: "android." + kind, ExtractorVersion: "android-v1", InputDigest: strings.Repeat("a", 64)}, data, nil); err != nil {
			t.Fatalf("%s normalized fixture violates surface contract: %v", kind, err)
		}
	}
}

// androidProject copies only repository sources; local SDK paths and Gradle
// outputs from the separate real smoke must not leak into ordinary tests.
func androidProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"packs/android", "examples/android"} {
		err := filepath.WalkDir(filepath.Join("../..", dir), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".gradle", "build", ".naru", "__pycache__":
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() == "local.properties" {
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
	write(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: code\npacks: [packs/android/pack.yaml]\nmodes: {code: [android.contracts]}\n")
	return root
}

func androidSeedBaseline(t *testing.T, root, name string) []byte {
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
	data, err := os.ReadFile("testdata/android/manifest.json")
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

func TestAndroidInvalidOutput(t *testing.T) {
	t.Run("adapter_negative_fixtures", func(t *testing.T) {
		androidFixtures(t, "ValidationTests", "BuildTests")
	})
	python := androidPython(t)
	for _, scenario := range []string{"missing_declared_tool", "present_tools_failed_build"} {
		t.Run(scenario, func(t *testing.T) {
			root := androidProject(t)
			bin := t.TempDir()
			if scenario == "present_tools_failed_build" {
				for _, tool := range []string{"java", "gradle", "sdkmanager"} {
					body := "#!/bin/sh\nexit 1\n"
					if tool == "gradle" {
						body = "#!/bin/sh\nprintf called > \"$ANDROID_TEST_CALLED\"\nexit 1\n"
					}
					path := filepath.Join(bin, tool)
					if err := os.WriteFile(path, []byte(body), 0o600); err != nil { // #nosec G703 -- Fixed fake-tool names in a test-owned bin directory.
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
			}
			t.Setenv("PATH", bin)
			t.Setenv("ANDROID_HOME", filepath.Join(root, "absent-sdk"))
			t.Setenv("ANDROID_TEST_CALLED", filepath.Join(root, "gradle-called"))
			approved := androidSeedBaseline(t, root, "android.manifest")
			if got := run(t, root, "", "check", "android.contracts"); got.code != 1 {
				t.Fatalf("unavailable/failing build passed: %+v", got)
			}
			state, err := receipt.NewStateStore(root).Current("android.contracts")
			if err != nil || state == nil || state.Execution.Result == nil {
				t.Fatalf("missing check result: %+v %v", state, err)
			}
			if scenario == "missing_declared_tool" && *state.Execution.Result != receipt.ResultNotRun {
				t.Fatalf("missing tool was not not-run: %+v", state)
			}
			if scenario == "present_tools_failed_build" && *state.Execution.Result != receipt.ResultFail {
				t.Fatalf("present tool build failure misclassified: %+v", state)
			}
			if got := run(t, root, "", "baseline", "extract", "android.manifest"); got.code != 1 {
				t.Fatalf("failed extraction accepted: %+v", got)
			}
			candidate, err := (baseline.Store{Root: root}).Current("android.manifest")
			if err != nil || candidate == nil || candidate.Phase != "failed" || candidate.Snapshot != nil {
				t.Fatalf("old candidate retained authority: %+v %v", candidate, err)
			}
			if got := run(t, root, "", "baseline", "diff", "android.manifest"); got.code != 1 {
				t.Fatalf("old candidate reused: %+v", got)
			}
			after, err := os.ReadFile(filepath.Join(root, ".naru/baselines/android.manifest.json")) // #nosec G304 -- Fixed test-owned baseline path.
			if err != nil || !bytes.Equal(approved, after) {
				t.Fatalf("failure mutated approved baseline: %v", err)
			}
		})
	}
}
