package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/receipt"
)

func project(t *testing.T, yaml string) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "in.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return root, cfg
}

const base = "version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks:\n  c:\n    inputs: [\"**\"]\n    run: "

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		run    string
		result string
		code   int
	}{
		{"exit 0", receipt.ResultPass, 0},
		{"exit 3", receipt.ResultFail, 3},
		{"kill -9 $$", receipt.ResultFail, -1}, // killed by a signal
	}
	for _, c := range cases {
		root, cfg := project(t, base+`"`+c.run+`"`)
		out, err := Run(root, cfg, "c", Options{Evidence: "local"})
		if err != nil {
			t.Fatalf("%s: %v", c.run, err)
		}
		if out.Receipt.Result != c.result || out.Receipt.ExitCode != c.code {
			t.Errorf("%s: got %s/%d, want %s/%d", c.run, out.Receipt.Result, out.Receipt.ExitCode, c.result, c.code)
		}
	}
}

// A background child that keeps stdout open must not turn a passing check into ERROR.
func TestBackgroundChildKeepingOutputOpen(t *testing.T) {
	old := waitDelay
	waitDelay = 200 * time.Millisecond
	defer func() { waitDelay = old }()
	root, cfg := project(t, base+`"sleep 5 & exit 0"`)
	start := time.Now()
	out, err := Run(root, cfg, "c", Options{Evidence: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Receipt.Result != receipt.ResultPass || out.Receipt.ExitCode != 0 {
		t.Fatalf("got %s/%d, want pass/0", out.Receipt.Result, out.Receipt.ExitCode)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("waited for the background child")
	}
	log, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(out.Receipt.LogPath)))
	if !strings.Contains(string(log), "background process kept its output open") {
		t.Fatalf("log does not explain the early return:\n%s", log)
	}
}

func TestTimeoutKillsProcessGroup(t *testing.T) {
	root, cfg := project(t, base+`"sh -c 'sleep 30; touch survived' & sleep 30"`+"\n    timeout: 300ms")
	out, err := Run(root, cfg, "c", Options{Evidence: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Receipt.Result != receipt.ResultTimeout {
		t.Fatalf("result = %s", out.Receipt.Result)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "survived")); err == nil {
		t.Fatal("grandchild survived the timeout")
	}
}

func TestEmptyInputsRefused(t *testing.T) {
	root, cfg := project(t, "version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks:\n  c:\n    inputs: [\"src/**\"]\n    run: \"true\"")
	_, err := Run(root, cfg, "c", Options{})
	if err == nil || !strings.Contains(err.Error(), "match no files") {
		t.Fatalf("empty inputs accepted: %v", err)
	}
}
