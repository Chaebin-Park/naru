package status

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runner"
)

// largeWorkspace fixes both file count and total bytes for comparable runs.
func largeWorkspace(tb testing.TB) (string, *config.Config) {
	tb.Helper()
	root := tb.TempDir()
	for _, dir := range []string{".naru", "src"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			tb.Fatal(err)
		}
	}
	definition := []byte("version: 1\ndefault_mode: code\nmodes: {code: [check]}\nchecks: {check: {run: 'true', inputs: [src/**]}}\n")
	if err := os.WriteFile(filepath.Join(root, ".naru/config.yaml"), definition, 0o600); err != nil {
		tb.Fatal(err)
	}
	const count, total = 10000, 100 * 1024 * 1024
	for i := 0; i < count; i++ {
		size := total / count
		if i < total%count {
			size++
		}
		name := filepath.Join(root, "src", fmt.Sprintf("%05d.txt", i))
		if err := os.WriteFile(name, bytes.Repeat([]byte{'a'}, size), 0o600); err != nil {
			tb.Fatal(err)
		} // #nosec G703 -- Generated numeric filenames below the test-owned temporary root.
	}
	cfg, err := config.Load(root)
	if err != nil {
		tb.Fatal(err)
	}
	out, err := runner.Run(root, cfg, "check", runner.Options{})
	if err != nil || out.Record == nil || out.Record.Execution.Result == nil || *out.Record.Execution.Result != receipt.ResultPass {
		tb.Fatalf("fixture check: %+v %v", out, err)
	}
	return root, cfg
}

func TestLargeWorkspaceStatus(t *testing.T) {
	root, cfg := largeWorkspace(t)
	assert := func(want State) {
		t.Helper()
		rep, err := Evaluate(root, cfg, "code")
		if err != nil || len(rep.Checks) != 1 || rep.Checks[0].State != want || rep.Satisfied != (want == Pass) {
			t.Fatalf("%+v %v", rep, err)
		}
	}
	assert(Pass)
	path := filepath.Join(root, "src", "00000.txt")
	original, err := os.ReadFile(path) // #nosec G304 -- Fixed numeric fixture path under the test-owned temporary root.
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	assert(Stale)
	if err := os.WriteFile(path, original, 0o600); err != nil { // #nosec G703 -- Restores bytes to the same fixed test-owned fixture path; file content never selects the path.
		t.Fatal(err)
	}
	// Current-state hashing cannot detect an intermediate change restored before evaluation.
	assert(Pass)
}

func BenchmarkLargeWorkspaceStatus(b *testing.B) {
	root, cfg := largeWorkspace(b)
	b.ReportAllocs()
	b.SetBytes(100 * 1024 * 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := Evaluate(root, cfg, "code")
		if err != nil || !rep.Satisfied {
			b.Fatalf("%+v %v", rep, err)
		}
	}
}
