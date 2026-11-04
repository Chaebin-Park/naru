package baseline

import (
	"context"
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

func approvalFixture(t *testing.T) (string, *Report, *surface.Snapshot) {
	t.Helper()
	root := t.TempDir()
	putFile(t, root, ".naru/config.yaml", []byte("version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks: {c: {run: 'true', inputs: [in.txt]}}\nsurfaces: {api: {run: 'true', inputs: [in.txt], extractor_version: v1}}\n"))
	putFile(t, root, "in.txt", []byte("input"))
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := fileset.Compute(root, cfg.Surfaces["api"].Inputs, cfg.Exclude, cfg.SurfaceFingerprint("api"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := surface.Build(surface.Request{Schema: 1, SurfaceID: "api", ExtractorVersion: "v1", InputDigest: strings.TrimPrefix(hash.Hash, "sha256:")}, []byte(`{"schema":1,"items":[]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Store{Root: root}).Put(State{Schema: 1, SurfaceID: "api", Phase: "ready", Snapshot: s}); err != nil {
		t.Fatal(err)
	}
	r, err := Diff(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	return root, r, s
}
func TestApprovalLifecycle(t *testing.T) {
	root, preview, snapshot := approvalFixture(t)
	record, err := Approve(context.Background(), root, "api", *preview, "Reviewer", "Initial empty surface reviewed")
	if err != nil {
		t.Fatal(err)
	}
	if record.Approval.OldDigest != nil || record.Approval.NewDigest != snapshot.Digest || record.Approval.InputDigest != snapshot.InputDigest || record.Approval.Reviewer != "Reviewer" || record.Approval.Reason == "" || record.Approval.ApprovedAt == "" {
		t.Fatalf("metadata %+v", record)
	}
	r, err := Diff(root, "api")
	if err != nil || r.State != "unchanged" {
		t.Fatalf("diff %+v %v", r, err)
	}
	if _, err := Approve(context.Background(), root, "api", *preview, "Reviewer", "Stale preview"); !errors.Is(err, ErrStale) {
		t.Fatalf("changed baseline accepted: %v", err)
	}
	saved, _, err := (Store{Root: root}).approved("api")
	if err != nil || saved.Approval.ID != record.Approval.ID {
		t.Fatalf("readback %+v %v", saved, err)
	}
	record, err = Approve(context.Background(), root, "api", *r, "Second reviewer", "Review again")
	if err != nil || record.Approval.OldDigest == nil || *record.Approval.OldDigest != snapshot.Digest {
		t.Fatalf("old digest %+v %v", record, err)
	}
	putFile(t, root, "in.txt", []byte("changed"))
	if _, err := Approve(context.Background(), root, "api", *r, "Reviewer", "Changed input"); !errors.Is(err, ErrStale) {
		t.Fatalf("changed inputs accepted: %v", err)
	}
}
func TestApprovalPreviewChanges(t *testing.T) {
	for _, change := range []string{"candidate", "definition", "failed", "legacy"} {
		t.Run(change, func(t *testing.T) {
			root, preview, snapshot := approvalFixture(t)
			switch change {
			case "candidate":
				next, err := surface.Build(surface.Request{Schema: 1, SurfaceID: "api", ExtractorVersion: "v1", InputDigest: snapshot.InputDigest}, []byte(`{"schema":1,"items":[{"id":"new","value":{}}]}`), nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := (Store{Root: root}).Put(State{Schema: 1, SurfaceID: "api", Phase: "ready", Snapshot: next}); err != nil {
					t.Fatal(err)
				}
			case "definition":
				path := filepath.Join(root, ".naru/config.yaml")
				data, err := os.ReadFile(path) // #nosec G304 -- Test-owned temporary root and fixed configuration fixture.
				if err != nil {
					t.Fatal(err)
				}
				putFile(t, root, ".naru/config.yaml", []byte(strings.ReplaceAll(string(data), "extractor_version: v1", "extractor_version: v2")))
			case "failed":
				if err := (Store{Root: root}).Put(State{Schema: 1, SurfaceID: "api", Phase: "failed", Reason: "failed"}); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				data, err := json.Marshal(snapshot)
				if err != nil {
					t.Fatal(err)
				}
				putFile(t, root, ".naru/baselines/api.json", data)
				r, err := Diff(root, "api")
				if err != nil || r.State != "approval-required" {
					t.Fatalf("legacy authority %+v %v", r, err)
				}
			}
			if _, err := Approve(context.Background(), root, "api", *preview, "Reviewer", "Changed preview"); err == nil {
				t.Fatal("changed preview approved")
			}
			if change == "legacy" {
				r, err := Diff(root, "api")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Approve(context.Background(), root, "api", *r, "Reviewer", "Migrate legacy record"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestApprovalPublicationFailure(t *testing.T) {
	root, preview, _ := approvalFixture(t)
	if _, err := Approve(context.Background(), root, "api", *preview, "Reviewer", "Initial"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".naru/baselines/api.json")
	before, err := read(path)
	if err != nil {
		t.Fatal(err)
	}
	denied := errors.New("rename denied")
	if err := publish(path, []byte(`{"partial":true}`), func(string, string) error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	r, err := Diff(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Approve(ctx, root, "api", *r, "Reviewer", "Cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, err := read(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("prior baseline lost")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files leaked: %v %v", entries, err)
	}
}
func TestApprovalMalformedRecords(t *testing.T) {
	root, preview, _ := approvalFixture(t)
	record, err := Approve(context.Background(), root, "api", *preview, "Reviewer", "Initial")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(data)
	for _, raw := range []string{
		strings.Replace(valid, `"schema":1`, `"schema":1,"schema":1`, 1),
		strings.Replace(valid, `"reviewer":"Reviewer"`, `"reviewer":"Reviewer","reviewer":"Reviewer"`, 1),
		strings.Replace(valid, `"reviewer":"Reviewer"`, `"reviewer":null`, 1),
		strings.Replace(valid, `"reason":"Initial"`, `"reason":""`, 1),
		strings.Replace(valid, `"new_digest":"`+record.Approval.NewDigest, `"new_digest":"`+strings.Repeat("a", 64), 1),
		strings.Replace(valid, `"id":"`+record.Approval.ID, `"id":"invalid`, 1),
		valid + `{}`,
	} {
		if _, err := decodeRecord([]byte(raw), "api"); err == nil {
			t.Fatalf("invalid approval accepted: %s", raw)
		}
	}
}
