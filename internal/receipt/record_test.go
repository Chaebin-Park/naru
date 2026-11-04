package receipt

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recordFixture() Record {
	started := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	return Record{Schema: StateSchema, Check: "test", RunID: strings.Repeat("a", 32), Phase: PhaseRunning, StartedAt: started,
		Provenance: Provenance{EvidenceLocal}, Execution: Execution{Command: "true", Workdir: ".", TimeoutMS: 1000, LogPath: ".naru/logs/test-" + strings.Repeat("a", 32) + ".log", NaruVersion: "dev"}, Inputs: Inputs{Policy: InputPolicy, Before: InputDigest{"sha256:" + strings.Repeat("b", 64), 1, "walk"}, Validity: InputsPending}}
}
func terminalFixture() Record {
	r := recordFixture()
	end := r.StartedAt.Add(time.Second)
	result, code, duration := ResultPass, 0, int64(1000)
	r.Phase = PhaseTerminal
	r.FinishedAt = &end
	r.Execution.Result = &result
	r.Execution.ExitCode = &code
	r.Execution.DurationMS = &duration
	after := r.Inputs.Before
	r.Inputs.After = &after
	r.Inputs.Validity = InputsMatched
	return r
}
func jsonRecord(t *testing.T, r Record) []byte {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestStrictRecordDecode(t *testing.T) {
	r := terminalFixture()
	original := string(jsonRecord(t, r))
	cases := map[string]string{
		"missing":          strings.Replace(original, `"exit_code":0,`, "", 1),
		"null required":    strings.Replace(original, `"command":"true"`, `"command":null`, 1),
		"duplicate":        strings.Replace(original, `"schema":2`, `"schema":2,"schema":2`, 1),
		"nested duplicate": strings.Replace(original, `"kind":"local"`, `"kind":"local","kind":"ci"`, 1),
		"extra":            strings.Replace(original, `"schema":2`, `"extra":0,"schema":2`, 1),
		"trailing":         original + ` {}`,
		"type":             strings.Replace(original, `"schema":2`, `"schema":"2"`, 1),
		"version":          strings.Replace(original, `"schema":2`, `"schema":3`, 1),
		"policy":           strings.Replace(original, InputPolicy, "other-policy", 1),
		"pass exit":        strings.Replace(original, `"exit_code":0`, `"exit_code":7`, 1),
		"run ID":           strings.Replace(original, r.RunID, "../oops", 1),
		"hash":             strings.Replace(original, `sha256:`, "other:", 1),
		"empty":            strings.Replace(original, `"file_count":1`, `"file_count":0`, 1),
		"null finished":    strings.Replace(original, `"finished_at":"2026-11-04T00:00:01Z"`, `"finished_at":null`, 1),
		"missing finished": strings.Replace(original, `"finished_at":"2026-11-04T00:00:01Z",`, "", 1),
		"offset":           strings.Replace(original, "00:00:00Z", "00:00:00+00:00", 1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRecord([]byte(data), "test"); err == nil {
				t.Fatal("accepted invalid record")
			}
		})
	}
	for _, r := range []Record{recordFixture(), terminalFixture()} {
		if _, err := DecodeRecord(jsonRecord(t, r), "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := DecodeRecord([]byte(original), "other"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestEveryRequiredRecordField(t *testing.T) {
	var obj map[string]any
	if err := json.Unmarshal(jsonRecord(t, terminalFixture()), &obj); err != nil {
		t.Fatal(err)
	}
	var visit func(map[string]any, string)
	visit = func(m map[string]any, path string) {
		for key, value := range m {
			delete(m, key)
			b, err := json.Marshal(obj)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeRecord(b, "test"); err == nil {
				t.Errorf("accepted missing %s%s", path, key)
			}
			m[key] = value
			if child, ok := value.(map[string]any); ok {
				visit(child, path+key+".")
			}
		}
	}
	visit(obj, "")
}
func TestStateAuthorityAndHistory(t *testing.T) {
	root := t.TempDir()
	s := NewStateStore(root)
	first := terminalFixture()
	if err := s.Put(first); err != nil {
		t.Fatal(err)
	}
	next := recordFixture()
	next.RunID = strings.Repeat("c", 32)
	next.Execution.LogPath = ".naru/logs/test-" + next.RunID + ".log"
	if err := s.Begin(next); err != nil {
		t.Fatal(err)
	}
	e, err := s.Read("test")
	if err != nil || e.Record.RunID != next.RunID || e.Legacy != nil {
		t.Fatalf("%+v %v", e, err)
	}
	archive := filepath.Join(root, ".naru/state/history/test", first.RunID+".json")
	before, err := os.ReadFile(archive) // #nosec G304 -- Test-owned history fixture.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRecord(before, "test"); err != nil {
		t.Fatal(err)
	}
	// A conflicting archive may never be overwritten.
	if err := s.Put(first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(next); err == nil {
		t.Fatal("replaced conflicting history")
	}
	// Even a valid old receipt cannot replace corrupt schema 2 authority.
	if err := NewStore(root).Put(Receipt{Schema: Schema, Check: "test", Result: ResultPass}); err != nil {
		t.Fatal(err)
	}
	path, _ := s.path("test")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("test"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fallback: %v", err)
	}
}
func TestStateRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	s := NewStateStore(root)
	if err := os.MkdirAll(filepath.Join(root, ".naru/state/checks"), 0o700); err != nil {
		t.Fatal(err)
	}
	path, _ := s.path("test")
	if err := os.Symlink("missing", path); err != nil {
		t.Skip(err)
	}
	if _, err := s.Read("test"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.Put(recordFixture()); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

type faultyState struct {
	*os.File
	failure string
}

var errState = errors.New("injected state IO failure")

func (f faultyState) Write(b []byte) (int, error) {
	if f.failure == "write" {
		return 0, errState
	}
	return f.File.Write(b)
}
func (f faultyState) Sync() error {
	if f.failure == "sync" {
		return errState
	}
	return f.File.Sync()
}
func (f faultyState) Close() error {
	err := f.File.Close()
	if f.failure == "close" {
		return errors.Join(err, errState)
	}
	return err
}
func TestStatePublicationFailures(t *testing.T) {
	for _, failure := range []string{"write", "sync", "close", "directory-sync", "rename"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			s := NewStateStore(root)
			old := recordFixture()
			if err := s.Put(old); err != nil {
				t.Fatal(err)
			}
			s.createTemp = func(dir, pattern string) (stateFile, error) {
				f, err := os.CreateTemp(dir, pattern)
				if err != nil {
					return nil, err
				}
				return faultyState{f, failure}, nil
			}
			if failure == "directory-sync" {
				s.syncDir = func(string) error { return errState }
			}
			if failure == "rename" {
				s.rename = func(string, string) error { return errState }
			}
			if err := s.Put(terminalFixture()); !errors.Is(err, errState) {
				t.Fatal(err)
			}
			e, err := s.Read("test")
			if err != nil || e.Record.Phase != PhaseRunning {
				t.Fatalf("published partial completion: %+v %v", e, err)
			}
			entries, err := os.ReadDir(filepath.Join(root, ".naru/state/checks"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("temp leaked: %v", entries)
			}
		})
	}
}
