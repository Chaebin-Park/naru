package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/status"
)

func TestPackChangesInvalidateEvidence(t *testing.T) {
	root, _ := stateProject(t, "true")
	if err := os.MkdirAll(filepath.Join(root, "packs", "demo"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateWrite(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: m\npacks: [packs/demo/pack.yaml]\nmodes: {m: [demo.c]}\n")
	manifest := "schema: 1\nid: demo\nversion: v1\nchecks: {c: {run: 'true', inputs: [in.txt]}}\n"
	stateWrite(t, root, "packs/demo/pack.yaml", manifest)
	for _, change := range []string{"manifest", "asset"} {
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Run(root, cfg, "demo.c", Options{}); err != nil {
			t.Fatal(err)
		}
		rep, err := status.Evaluate(root, cfg, "m")
		if err != nil || !rep.Satisfied {
			t.Fatalf("initial pass: %+v %v", rep, err)
		}
		if change == "manifest" {
			stateWrite(t, root, "packs/demo/pack.yaml", strings.Replace(manifest, "v1", "v2", 1))
		} else {
			stateWrite(t, root, "packs/demo/helper.sh", "echo changed\n")
		}
		cfg, err = config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		rep, err = status.Evaluate(root, cfg, "m")
		if err != nil || rep.Satisfied || rep.Checks[0].State != status.Stale {
			t.Fatalf("pack change reused pass: %+v %v", rep, err)
		}
	}
}
