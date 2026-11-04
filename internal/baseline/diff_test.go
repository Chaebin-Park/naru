package baseline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/surface"
)

func putFile(t *testing.T, root, rel string, b []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	} // #nosec G703 -- Test-owned temporary root and fixed fixture-relative paths.
}
func TestCandidateDiffLifecycle(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, ".naru/config.yaml", []byte("version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks: {c: {run: 'true', inputs: [in.txt]}}\nsurfaces: {api: {run: 'true', inputs: [in.txt], extractor_version: v1}}\n"))
	putFile(t, root, "in.txt", []byte("input"))
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := fileset.Compute(root, cfg.Surfaces["api"].Inputs, cfg.Exclude, cfg.SurfaceFingerprint("api"))
	if err != nil {
		t.Fatal(err)
	}
	makeSnapshot := func(output string) *surface.Snapshot {
		t.Helper()
		s, err := surface.Build(surface.Request{Schema: 1, SurfaceID: "api", ExtractorVersion: "v1", InputDigest: strings.TrimPrefix(digest.Hash, "sha256:")}, []byte(output), nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := makeSnapshot(`{"schema":1,"items":[{"id":"remove","value":{}},{"id":"change","value":{"n":1}}]}`)
	store := Store{Root: root}
	if err := store.Put(State{Schema: 1, SurfaceID: "api", Phase: "ready", Snapshot: first}); err != nil {
		t.Fatal(err)
	}
	report, err := Diff(root, "api")
	if err != nil || report.State != "missing-baseline" || report.OldDigest != nil {
		t.Fatalf("missing %+v %v", report, err)
	}
	approved, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, root, ".naru/baselines/api.json", approved) // Synthetic approval fixture; production extract never writes here.
	report, err = Diff(root, "api")
	if err != nil || report.State != "unchanged" {
		t.Fatalf("same %+v %v", report, err)
	}
	next := makeSnapshot(`{"schema":1,"items":[{"id":"change","value":{"n":2}},{"id":"add","value":{}}]}`)
	if err := store.Put(State{Schema: 1, SurfaceID: "api", Phase: "ready", Snapshot: next}); err != nil {
		t.Fatal(err)
	}
	report, err = Diff(root, "api")
	if err != nil || report.State != "changed" || len(report.Added) != 1 || report.Added[0].ID != "add" || len(report.Removed) != 1 || report.Removed[0].ID != "remove" || len(report.Modified) != 1 || report.Modified[0].ID != "change" {
		t.Fatalf("diff %+v %v", report, err)
	}
	putFile(t, root, "in.txt", []byte("changed"))
	if _, err := Diff(root, "api"); !errors.Is(err, ErrStale) {
		t.Fatal("stale candidate accepted", err)
	}
	if err := store.Put(State{Schema: 1, SurfaceID: "api", Phase: "failed", Reason: "extractor_failed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Diff(root, "api"); !errors.Is(err, ErrNotReady) {
		t.Fatal("failed candidate accepted", err)
	}
	actual, err := read(filepath.Join(root, ".naru/baselines/api.json"))
	if err != nil || string(actual) != string(approved) {
		t.Fatal("baseline changed")
	}
	for _, raw := range []string{`{"schema":1,"schema":1,"surface_id":"api","phase":"failed","reason":"x","snapshot":null}`, `{"schema":1,"surface_id":"api","phase":"ready","reason":"","snapshot":null}`, `{"schema":1,"surface_id":"api","phase":"running","reason":null,"snapshot":null}`} {
		if _, err := decodeState([]byte(raw), "api"); err == nil {
			t.Fatal("malformed state accepted")
		}
	}
}

func TestCandidatePublicationFailure(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".naru", "state", "surfaces", "api.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	err := (Store{Root: root}).Put(State{Schema: 1, SurfaceID: "api", Phase: "running"})
	if !errors.Is(err, ErrStorage) {
		t.Fatal("publication failure hidden", err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "api.json" || !entries[0].IsDir() {
		t.Fatal("failed write left temporary state")
	}
}
