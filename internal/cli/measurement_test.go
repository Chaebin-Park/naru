package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLinuxColdStatusHelper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cold protocol uses POSIX descriptors")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 required for cold protocol verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// #nosec G204 -- Fixed synthetic protocol tests; no privileges or cache reset requested.
	output, err := exec.CommandContext(ctx, python, "-B", "../../scripts/test_linux_cold_status.py").CombinedOutput()
	if err != nil {
		t.Fatalf("cold status protocol: %v\n%s", err, output)
	}
}

func TestLinuxReaderGuardHelper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("reader guard protocol requires POSIX descriptors")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 required for reader guard verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// #nosec G204 -- Fixed repository-owned tests; no privileges or cache reset requested.
	output, err := exec.CommandContext(ctx, python, "-B", "../../scripts/test_linux_reader_guard.py").CombinedOutput()
	if err != nil {
		t.Fatalf("reader guard: %v\n%s", err, output)
	}
}

func TestStatusProvenanceHelper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("prepared workload provenance requires POSIX directory descriptors")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 required for provenance verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// #nosec G204 -- Fixed repository-owned suite, synthetic files and executables only.
	output, err := exec.CommandContext(ctx, python, "-B", "../../scripts/test_status_provenance.py").CombinedOutput()
	if err != nil {
		t.Fatalf("status provenance: %v\n%s", err, output)
	}
}

func TestStatusMeasurementHelper(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 required for measurement helper verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// #nosec G204 -- Named installed Python runs only the fixed repository-owned helper tests.
	output, err := exec.CommandContext(ctx, python, "-B", "../../scripts/test_measure_status.py").CombinedOutput()
	if err != nil {
		t.Fatalf("measurement helper: %v\n%s", err, output)
	}
}

func TestLinuxCacheProbeHelper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cache probe requires POSIX descriptor and permission semantics")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 required for cache probe fixture verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// #nosec G204 -- Fixed repository-owned Python test suite; no reset command is invoked.
	output, err := exec.CommandContext(ctx, python, "-B", "../../scripts/test_linux_cache_probe.py").CombinedOutput()
	if err != nil {
		t.Fatalf("cache probe helper: %v\n%s", err, output)
	}
}

// Linux CI proves wait4 collection; macOS also guards the existing time-l path.
func TestLinuxStatusMeasurementSmoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("status RSS collectors require Linux or macOS")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"NARU_MODE", "NARU_GATE", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(key, "")
	}
	binary := filepath.Join(t.TempDir(), "naru")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// #nosec G204 -- Fixed build command writes only a test-owned executable.
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/naru").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, mode := range []string{"warm-cache", "first-run"} {
		t.Run(mode, func(t *testing.T) {
			// #nosec G204 -- Repository-owned helper and test-built binary, fixed synthetic workload.
			out, err := exec.CommandContext(ctx, python, "-B", "../../scripts/measure-status.py", binary,
				"--cache-mode", mode, "--files", "4", "--bytes", "4096", "--checks", "2", "--samples", "1").CombinedOutput()
			if err != nil {
				t.Fatalf("measure: %v\n%s", err, out)
			}
			var report struct {
				BinarySHA  string `json:"binary_sha256"`
				CacheMode  string `json:"cache_mode"`
				Collector  string `json:"collector"`
				Samples    int    `json:"samples"`
				Warmups    int    `json:"warmups"`
				Provenance struct {
					Digest   string          `json:"manifest_sha256"`
					Manifest json.RawMessage `json:"manifest"`
				} `json:"provenance"`
				Rows []struct {
					Seconds float64 `json:"seconds"`
					RSS     int64   `json:"max_rss_bytes"`
					Digest  string  `json:"manifest_sha256"`
				} `json:"samples_data"`
			}
			if err := json.Unmarshal(out, &report); err != nil {
				t.Fatal(err)
			}
			collector, warmups := "macos-time-l", 0
			if runtime.GOOS == "linux" {
				collector = "linux-wait4"
			}
			if mode == "warm-cache" {
				warmups = 1
			}
			if report.CacheMode != mode || report.Collector != collector || report.Samples != 1 || report.Warmups != warmups || len(report.Rows) != 1 || report.Rows[0].Seconds <= 0 || report.Rows[0].RSS <= 0 {
				t.Fatalf("invalid measurement: %s", out)
			}
			// The helper emits sorted ASCII keys; Go's compact sorted-key encoding
			// is identical for this fixed synthetic manifest.
			var manifest map[string]any
			if err := json.Unmarshal(report.Provenance.Manifest, &manifest); err != nil {
				t.Fatal(err)
			}
			canonical, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(canonical))
			if report.Provenance.Digest != digest || report.Rows[0].Digest != digest {
				t.Fatalf("sample not bound to manifest: %s", out)
			}
			// #nosec G304 -- Fixed filename in t.TempDir, built by this test; no external path input.
			binaryBytes, err := os.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}
			var inventory struct {
				Binary struct {
					SHA string `json:"sha256"`
				} `json:"binary"`
				Files []struct {
					Path string `json:"path"`
				} `json:"files"`
			}
			if err := json.Unmarshal(report.Provenance.Manifest, &inventory); err != nil {
				t.Fatal(err)
			}
			if inventory.Binary.SHA != fmt.Sprintf("%x", sha256.Sum256(binaryBytes)) || report.BinarySHA != inventory.Binary.SHA {
				t.Fatal("manifest does not identify the executed test binary")
			}
			paths := make(map[string]bool)
			for _, entry := range inventory.Files {
				paths[entry.Path] = true
			}
			for _, required := range []string{".naru/config.yaml", ".naru/state/checks/check0.json", ".naru/state/checks/check1.json", "src/00000.txt", "src/00001.txt", "src/00002.txt", "src/00003.txt"} {
				if !paths[required] {
					t.Fatalf("manifest omits %s", required)
				}
			}
			t.Logf("collector=%s mode=%s seconds=%g rss_bytes=%d", collector, mode, report.Rows[0].Seconds, report.Rows[0].RSS)
		})
	}
}
