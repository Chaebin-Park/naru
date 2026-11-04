package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

func TestLegacyPolicyAndNewAuthority(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "in.txt"), []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse([]byte("version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks: {c: {run: 'true', inputs: [in.txt]}}"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := fileset.Compute(root, []string{"in.txt"}, cfg.Exclude, cfg.Fingerprint("c"))
	if err != nil {
		t.Fatal(err)
	}
	r := receipt.Receipt{Schema: 1, Check: "c", Command: "true", InputsHash: d.Hash, FileCount: d.FileCount, FileSource: string(d.Source), Result: "pass", Evidence: "local", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), LogPath: "old.log", NaruVersion: "old"}
	if err := receipt.NewStore(root).Put(r); err != nil {
		t.Fatal(err)
	}
	rep, err := EvaluateState(root, cfg, "m")
	if err != nil || rep.Satisfied || rep.Checks[0].State != Incompatible || rep.Checks[0].Receipt.Evidence != "local" {
		t.Fatalf("legacy pass accepted: %+v %v", rep, err)
	}
	lock, err := runlock.Acquire(root, "c")
	if err != nil {
		t.Fatal(err)
	}
	rep, err = EvaluateState(root, cfg, "m")
	if err != nil || rep.Checks[0].State != Running {
		t.Fatalf("active execution reused legacy: %+v %v", rep, err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".naru/state/checks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".naru/state/checks/c.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".naru/state/unverified.json"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err = EvaluateState(root, cfg, "m")
	if err != nil || rep.Satisfied || rep.Checks[0].State != Invalid || !rep.Unverified.Present {
		t.Fatalf("corrupt authority: %+v %v", rep, err)
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"schema":2`, `"run_id":null`, `"reason":"invalid_receipt"`, `"unverified":{"present":true`, `"provenance":null`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("missing contract %s: %s", key, data)
		}
	}
}
