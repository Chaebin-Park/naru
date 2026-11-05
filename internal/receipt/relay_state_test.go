package receipt

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pendingRelayFixture(t *testing.T, id string) RelayRecord {
	t.Helper()
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	p := RelayPacket{Schema: 1, PacketID: strings.Repeat(id, 32), Check: "test", IssuedAt: now, ExpiresAt: now.Add(time.Hour), InputDigest: InputDigest{"sha256:" + strings.Repeat("b", 64), 1, "walk"}, DefinitionHash: "sha256:" + strings.Repeat("c", 64), Command: "true", Workdir: ".", TimeoutMS: 1000, Inputs: []string{"in.txt"}, Excludes: []string{}, Requirements: RelayRequirements{Tools: []string{}, OS: []string{}}, Target: "public fixture", ExpectedArtifacts: []string{"execution.log"}}
	hash, err := PacketHash(p)
	if err != nil {
		t.Fatal(err)
	}
	return RelayRecord{Schema: RelaySchema, InputPolicy: RelayInputPolicy, Check: "test", RunID: p.PacketID, Phase: PhasePending, Provenance: Provenance{Kind: EvidenceHumanRelayed}, Packet: p, PacketHash: hash}
}
func terminalRelayFixture(t *testing.T, r RelayRecord) RelayRecord {
	t.Helper()
	zero := 0
	r.Phase = PhaseTerminal
	r.Acceptance = &RelayAcceptance{AcceptedAt: r.Packet.IssuedAt.Add(3 * time.Second), SubmittedBy: "operator", StartedAt: r.Packet.IssuedAt.Add(time.Second), FinishedAt: r.Packet.IssuedAt.Add(2 * time.Second), BeforeInputDigest: r.Packet.InputDigest, AfterInputDigest: r.Packet.InputDigest, Result: ResultPass, ExitCode: &zero, ToolVersions: []RelayToolVersion{{Name: "runtime", Version: "1"}}, Artifacts: []RelayArtifact{{Name: "execution.log", SHA256: "sha256:" + strings.Repeat("d", 64), StoredPath: ".naru/relay/artifacts/" + r.RunID + "/" + strings.Repeat("e", 32) + "/execution.log", Size: 1}}}
	if err := r.Validate("test"); err != nil {
		t.Fatal(err)
	}
	return r
}
func relayStateBytes(t *testing.T, s StateStore) []byte {
	t.Helper()
	path, err := s.path("test")
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.read(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestRelayArchivePublicationFailure(t *testing.T) {
	root := t.TempDir()
	store := NewStateStore(root)
	a := pendingRelayFixture(t, "a")
	if err := store.BeginRelay(a); err != nil {
		t.Fatal(err)
	}
	pending := relayStateBytes(t, store)
	failed := store
	failed.rename = func(from, to string) error {
		if to == filepath.Join(root, ".naru/state/checks/test.json") {
			return errors.New("injected authority publication failure")
		}
		return os.Rename(from, to)
	}
	b := pendingRelayFixture(t, "b")
	if err := failed.BeginRelay(b); err == nil {
		t.Fatal("publication fault ignored")
	}
	if !bytes.Equal(relayStateBytes(t, store), pending) {
		t.Fatal("pending A authority changed")
	}
	a = terminalRelayFixture(t, a)
	if err := store.PutRelay(a); err != nil {
		t.Fatal(err)
	}
	terminal := relayStateBytes(t, store)
	c := pendingRelayFixture(t, "c")
	if err := store.BeginRelay(c); err != nil {
		t.Fatal(err)
	}
	for phase, data := range map[string][]byte{PhasePending: pending, PhaseTerminal: terminal} {
		name := phase + "-" + strings.TrimPrefix(hashRelayBytes(data), "sha256:") + ".json"
		archived, err := store.read(filepath.Join(root, ".naru/state/history/test/relay", a.RunID, name))
		if err != nil || !bytes.Equal(data, archived) {
			t.Fatalf("lost %s archive: %v", phase, err)
		}
	}
	current, err := store.CurrentEvidence("test")
	if err != nil || current.Relay == nil || current.Relay.RunID != c.RunID {
		t.Fatalf("C not current: %+v %v", current, err)
	}
}
func hashRelayBytes(data []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(data)) }

func TestRelayPublicationFailurePreservesAuthority(t *testing.T) {
	for _, fault := range []string{"rename", "sync"} {
		t.Run(fault, func(t *testing.T) {
			store := NewStateStore(t.TempDir())
			pending := pendingRelayFixture(t, "a")
			if err := store.BeginRelay(pending); err != nil {
				t.Fatal(err)
			}
			before := relayStateBytes(t, store)
			broken := store
			if fault == "rename" {
				broken.rename = func(string, string) error { return errors.New("rename failed") }
			} else {
				broken.syncDir = func(string) error { return errors.New("sync failed") }
			}
			terminal := terminalRelayFixture(t, pending)
			if err := broken.PutRelay(terminal); err == nil {
				t.Fatal("fault ignored")
			}
			if !bytes.Equal(before, relayStateBytes(t, store)) {
				t.Fatal("fault replaced authority")
			}
			if err := store.PutRelay(terminal); err != nil {
				t.Fatal(err)
			}
			local := recordFixture()
			local.RunID = strings.Repeat("f", 32)
			local.Execution.LogPath = ".naru/logs/test-" + local.RunID + ".log"
			if err := store.Begin(local); err != nil {
				t.Fatal(err)
			}
			current, err := store.Current("test")
			if err != nil || current.RunID != local.RunID {
				t.Fatalf("local run not authoritative: %+v %v", current, err)
			}
		})
	}
}
func TestRelayStrictNestedFields(t *testing.T) {
	r := terminalRelayFixture(t, pendingRelayFixture(t, "a"))
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	original := string(data)
	cases := map[string]string{
		"nested missing size": strings.Replace(original, `,"size":1`, "", 1),
		"nested null version": strings.Replace(original, `"version":"1"`, `"version":null`, 1),
		"nested unknown":      strings.Replace(original, `"version":"1"`, `"version":"1","extra":0`, 1),
		"case alias":          strings.Replace(original, `"schema":3`, `"schema":3,"Schema":3`, 1),
		"nested case alias":   strings.Replace(original, `"size":1`, `"size":1,"Size":1`, 1),
		"duplicate":           strings.Replace(original, `"size":1`, `"size":1,"size":1`, 1),
		"null acceptance":     strings.Replace(string(mustRelayJSON(t, pendingRelayFixture(t, "a"))), `"phase":"pending"`, `"phase":"pending","acceptance": null`, 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRelayRecord([]byte(input), "test"); err == nil {
				t.Fatal("invalid nested record accepted")
			}
		})
	}
	if _, err := DecodeRelayRecord(data, "test"); err != nil {
		t.Fatal(err)
	}
}
func mustRelayJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRelayPostPublicationSyncFailureKeepsConsumedAuthority(t *testing.T) {
	store := NewStateStore(t.TempDir())
	pending := pendingRelayFixture(t, "a")
	if err := store.BeginRelay(pending); err != nil {
		t.Fatal(err)
	}
	calls := 0
	broken := store
	broken.syncDir = func(string) error {
		calls++
		if calls == 2 {
			return errors.New("post-publication sync failure")
		}
		return nil
	}
	terminal := terminalRelayFixture(t, pending)
	if err := broken.PutRelay(terminal); err == nil {
		t.Fatal("sync failure hidden")
	}
	e, err := store.CurrentEvidence("test")
	if err != nil || e.Relay == nil || e.Relay.Phase != PhaseTerminal {
		t.Fatalf("committed consumption erased: %+v %v", e, err)
	}
}
