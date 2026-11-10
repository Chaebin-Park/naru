package cli

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

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
				CacheMode string `json:"cache_mode"`
				Collector string `json:"collector"`
				Samples   int    `json:"samples"`
				Warmups   int    `json:"warmups"`
				Rows      []struct {
					Seconds float64 `json:"seconds"`
					RSS     int64   `json:"max_rss_bytes"`
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
			t.Logf("collector=%s mode=%s seconds=%g rss_bytes=%d", collector, mode, report.Rows[0].Seconds, report.Rows[0].RSS)
		})
	}
}
