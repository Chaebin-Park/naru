package cli

import (
	"context"
	"os/exec"
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
