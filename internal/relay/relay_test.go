package relay_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/relay"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

const relayConfig = `version: 1
default_mode: code
modes: {code: [remote]}
exclude: [ignored.txt]
checks:
  remote:
    run: "touch must-not-run"
    workdir: .
    timeout: 30s
    inputs: [input.txt]
    requires: {tools: [naru-relay-missing-tool], os: [linux]}
`

type relayFixture struct {
	root   string
	now    time.Time
	packet receipt.RelayPacket
}

func relayWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil { // #nosec G703 -- Test-owned temporary paths; no external filesystem input.
		t.Fatal(err)
	}
}
func relayRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- Test-owned fixture and authority paths only.
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func relayJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func relayHash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func freshRelay(t *testing.T) *relayFixture {
	t.Helper()
	f := &relayFixture{root: t.TempDir(), now: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	relayWrite(t, filepath.Join(f.root, ".naru/config.yaml"), []byte(relayConfig))
	relayWrite(t, filepath.Join(f.root, "input.txt"), []byte("public input\n"))
	p, err := relay.New(f.root, "remote", f.options())
	if err != nil {
		t.Fatal(err)
	}
	f.packet = p
	return f
}
func (f *relayFixture) options() relay.NewOptions {
	return relay.NewOptions{Target: "synthetic remote", Artifacts: []string{"execution.log"}, Now: func() time.Time { return f.now }}
}
func (f *relayFixture) authority(t *testing.T) []byte {
	t.Helper()
	return relayRead(t, filepath.Join(f.root, ".naru/state/checks/remote.json"))
}
func (f *relayFixture) submission(t *testing.T) (string, receipt.RelaySubmission) {
	t.Helper()
	dir := filepath.Join(f.root, "submission")
	data := []byte("synthetic execution output\n")
	relayWrite(t, filepath.Join(dir, "execution.log"), data)
	zero := 0
	s := receipt.RelaySubmission{Schema: 1, PacketID: f.packet.PacketID, PacketHash: relayHash(relayJSON(t, f.packet)), Check: "remote", SubmittedBy: "test operator", StartedAt: f.now.Add(time.Second), FinishedAt: f.now.Add(2 * time.Second), BeforeInputDigest: f.packet.InputDigest, AfterInputDigest: f.packet.InputDigest, Result: "pass", ExitCode: &zero, ToolVersions: []receipt.RelayToolVersion{{Name: "test-runtime", Version: "1.0"}}, Artifacts: []receipt.RelayArtifactInput{{Name: "execution.log", Path: "execution.log", SHA256: relayHash(data)}}}
	path := filepath.Join(dir, "submission.json")
	relayWrite(t, path, relayJSON(t, s))
	return path, s
}
func (f *relayFixture) submit(path string) (receipt.RelayRecord, error) {
	return relay.Submit(f.root, f.packet.PacketID, path, relay.SubmitOptions{Now: func() time.Time { return f.now.Add(3 * time.Second) }})
}
func unchangedAuthority(t *testing.T, f *relayFixture, before []byte) {
	t.Helper()
	if !bytes.Equal(before, f.authority(t)) {
		t.Fatal("rejected operation changed authority")
	}
}

func TestRelayNew(t *testing.T) {
	t.Run("binds without execution", func(t *testing.T) {
		f := freshRelay(t)
		p := f.packet
		cfg, err := config.Load(f.root)
		if err != nil {
			t.Fatal(err)
		}
		c := cfg.Checks["remote"]
		digest, err := fileset.Compute(f.root, c.Inputs, cfg.Exclude, cfg.Fingerprint("remote"))
		if err != nil {
			t.Fatal(err)
		}
		if p.Check != "remote" || p.Command != c.Run || p.Workdir != "." || p.TimeoutMS != 30000 || !reflect.DeepEqual(p.Inputs, c.Inputs) || !reflect.DeepEqual(p.Excludes, cfg.Exclude) || !reflect.DeepEqual(p.Requirements.Tools, c.Requires.Tools) || !reflect.DeepEqual(p.Requirements.OS, c.Requires.OS) {
			t.Fatalf("definition not bound: %+v", p)
		}
		if p.InputDigest.Hash != digest.Hash || p.InputDigest.FileCount != digest.FileCount || p.DefinitionHash != relayHash([]byte(cfg.Fingerprint("remote"))) {
			t.Fatalf("digest not bound: %+v", p)
		}
		if p.ExpiresAt.Sub(p.IssuedAt) != 24*time.Hour || p.IssuedAt != f.now || len(p.PacketID) != 32 {
			t.Fatalf("identity/expiry: %+v", p)
		}
		if _, err := os.Stat(filepath.Join(f.root, "must-not-run")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("relay executed command: %v", err)
		}
		r, err := receipt.DecodeRelayRecord(f.authority(t), "remote")
		if err != nil {
			t.Fatal(err)
		}
		if r.Phase != "pending" || r.Acceptance != nil || r.PacketHash != relayHash(relayJSON(t, p)) {
			t.Fatalf("not pending: %+v", r)
		}
		exported := relayRead(t, filepath.Join(f.root, ".naru/relay/packets", p.PacketID+".json"))
		var copy receipt.RelayPacket
		if err := json.Unmarshal(exported, &copy); err != nil || !reflect.DeepEqual(copy, p) {
			t.Fatalf("packet export mismatch: %v", err)
		}
	})
	t.Run("new request replaces pass", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		if _, err := f.submit(path); err != nil {
			t.Fatal(err)
		}
		old := f.authority(t)
		p, err := relay.New(f.root, "remote", f.options())
		if err != nil {
			t.Fatal(err)
		}
		if p.PacketID == f.packet.PacketID {
			t.Fatal("packet ID reused")
		}
		r, err := receipt.DecodeRelayRecord(f.authority(t), "remote")
		if err != nil || r.Phase != "pending" || r.Acceptance != nil {
			t.Fatalf("older pass remained authority: %+v %v", r, err)
		}
		matches, err := filepath.Glob(filepath.Join(f.root, ".naru/state/history/remote/relay", f.packet.PacketID, "terminal-*.json"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("terminal history missing: %v %v", matches, err)
		}
		if !bytes.Equal(old, relayRead(t, matches[0])) {
			t.Fatal("archival changed original bytes")
		}
	})
	t.Run("invalid options preserve pending", func(t *testing.T) {
		for _, change := range []func(*relay.NewOptions){
			func(o *relay.NewOptions) { o.TTL = -time.Second }, func(o *relay.NewOptions) { o.TTL = 7*24*time.Hour + time.Second },
			func(o *relay.NewOptions) { o.Target = "" }, func(o *relay.NewOptions) { o.Artifacts = nil }, func(o *relay.NewOptions) { o.Artifacts = []string{"../escape"} }, func(o *relay.NewOptions) { o.Artifacts = []string{"a", "a"} },
		} {
			f := freshRelay(t)
			before := f.authority(t)
			opt := f.options()
			change(&opt)
			if _, err := relay.New(f.root, "remote", opt); err == nil {
				t.Fatal("accepted invalid options")
			}
			unchangedAuthority(t, f, before)
		}
	})
	t.Run("lock and cancellation", func(t *testing.T) {
		f := freshRelay(t)
		before := f.authority(t)
		lock, err := runlock.Acquire(f.root, "remote")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := relay.New(f.root, "remote", f.options()); err == nil {
			t.Error("ignored execution lock")
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		unchangedAuthority(t, f, before)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		opt := f.options()
		opt.Context = ctx
		if _, err := relay.New(f.root, "remote", opt); err == nil {
			t.Fatal("ignored cancellation")
		}
		unchangedAuthority(t, f, before)
	})
}

func TestRelaySubmit(t *testing.T) {
	t.Run("accept copies artifact and preserves provenance", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		r, err := f.submit(path)
		if err != nil {
			t.Fatal(err)
		}
		if r.Provenance.Kind != "human-relayed" || r.Phase != "terminal" || r.Acceptance == nil || r.Acceptance.Result != "pass" || r.Acceptance.SubmittedBy != "test operator" {
			t.Fatalf("acceptance: %+v", r)
		}
		if len(r.Acceptance.Artifacts) != 1 {
			t.Fatal("missing copied artifact")
		}
		a := r.Acceptance.Artifacts[0]
		relayWrite(t, filepath.Join(filepath.Dir(path), "execution.log"), []byte("source changed"))
		stored := relayRead(t, filepath.Join(f.root, filepath.FromSlash(a.StoredPath)))
		if relayHash(stored) != a.SHA256 || int64(len(stored)) != a.Size {
			t.Fatal("accepted source was not copied")
		}
		if err := receipt.ValidateRelayEvidence(f.root, r, f.now.Add(4*time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(f.root, "must-not-run")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("submission ran command: %v", err)
		}
	})
	t.Run("nonpass consumes request", func(t *testing.T) {
		f := freshRelay(t)
		path, s := f.submission(t)
		code := 7
		s.Result = "fail"
		s.ExitCode = &code
		relayWrite(t, path, relayJSON(t, s))
		r, err := f.submit(path)
		if err != nil || r.Acceptance == nil || r.Acceptance.Result != "fail" {
			t.Fatalf("nonpass lost: %+v %v", r, err)
		}
		before := f.authority(t)
		s.Result = "pass"
		zero := 0
		s.ExitCode = &zero
		relayWrite(t, path, relayJSON(t, s))
		if _, err := f.submit(path); err == nil {
			t.Fatal("rewrote failed evidence as pass")
		}
		unchangedAuthority(t, f, before)
	})
	cases := map[string]func(*receipt.RelaySubmission){
		"packet":             func(s *receipt.RelaySubmission) { s.PacketID = strings.Repeat("a", 32) },
		"packet hash":        func(s *receipt.RelaySubmission) { s.PacketHash = "sha256:" + strings.Repeat("0", 64) },
		"check":              func(s *receipt.RelaySubmission) { s.Check = "other" },
		"empty submitter":    func(s *receipt.RelaySubmission) { s.SubmittedBy = "" },
		"before digest":      func(s *receipt.RelaySubmission) { s.BeforeInputDigest.Hash = "sha256:" + strings.Repeat("0", 64) },
		"after count":        func(s *receipt.RelaySubmission) { s.AfterInputDigest.FileCount++ },
		"time order":         func(s *receipt.RelaySubmission) { s.FinishedAt = s.StartedAt.Add(-time.Second) },
		"future finish":      func(s *receipt.RelaySubmission) { s.FinishedAt = s.FinishedAt.Add(time.Hour) },
		"before issuance":    func(s *receipt.RelaySubmission) { s.StartedAt = s.StartedAt.Add(-time.Hour) },
		"pass exit":          func(s *receipt.RelaySubmission) { n := 7; s.ExitCode = &n },
		"pass null exit":     func(s *receipt.RelaySubmission) { s.ExitCode = nil },
		"no tools":           func(s *receipt.RelaySubmission) { s.ToolVersions = nil },
		"missing artifact":   func(s *receipt.RelaySubmission) { s.Artifacts = nil },
		"duplicate artifact": func(s *receipt.RelaySubmission) { s.Artifacts = append(s.Artifacts, s.Artifacts[0]) },
		"extra artifact": func(s *receipt.RelaySubmission) {
			a := s.Artifacts[0]
			a.Name = "extra.log"
			s.Artifacts = append(s.Artifacts, a)
		},
		"hash mismatch": func(s *receipt.RelaySubmission) { s.Artifacts[0].SHA256 = "sha256:" + strings.Repeat("0", 64) },
		"traversal":     func(s *receipt.RelaySubmission) { s.Artifacts[0].Path = "../input.txt" },
		"absolute":      func(s *receipt.RelaySubmission) { s.Artifacts[0].Path = "/tmp/execution.log" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := freshRelay(t)
			path, s := f.submission(t)
			before := f.authority(t)
			change(&s)
			relayWrite(t, path, relayJSON(t, s))
			if _, err := f.submit(path); err == nil {
				t.Fatal("accepted invalid submission")
			}
			unchangedAuthority(t, f, before)
		})
	}
	for name, change := range map[string]func(string) string{
		"unknown":   func(s string) string { return strings.Replace(s, `"schema":1`, `"unknown":true,"schema":1`, 1) },
		"duplicate": func(s string) string { return strings.Replace(s, `"schema":1`, `"schema":1,"schema":1`, 1) },
		"null": func(s string) string {
			return strings.Replace(s, `"submitted_by":"test operator"`, `"submitted_by":null`, 1)
		},
		"missing": func(s string) string { return strings.Replace(s, `"submitted_by":"test operator",`, "", 1) },
		"nested duplicate": func(s string) string {
			return strings.Replace(s, `"name":"execution.log"`, `"name":"execution.log","name":"execution.log"`, 1)
		},
		"trailing": func(s string) string { return s + ` {}` },
	} {
		t.Run(name, func(t *testing.T) {
			f := freshRelay(t)
			path, _ := f.submission(t)
			before := f.authority(t)
			relayWrite(t, path, []byte(change(string(relayRead(t, path)))))
			if _, err := f.submit(path); err == nil {
				t.Fatal("accepted malformed protocol")
			}
			unchangedAuthority(t, f, before)
		})
	}
	for _, kind := range []string{"missing", "symlink", "parent symlink", "directory", "oversized"} {
		t.Run("artifact "+kind, func(t *testing.T) {
			f := freshRelay(t)
			path, s := f.submission(t)
			before := f.authority(t)
			artifact := filepath.Join(filepath.Dir(path), "execution.log")
			switch kind {
			case "missing":
				if err := os.Remove(artifact); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(artifact); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(f.root, "input.txt"), artifact); err != nil {
					t.Fatal(err)
				}
			case "parent symlink":
				if err := os.Symlink(filepath.Dir(path), filepath.Join(filepath.Dir(path), "alias")); err != nil {
					t.Fatal(err)
				}
				s.Artifacts[0].Path = "alias/execution.log"
				relayWrite(t, path, relayJSON(t, s))
			case "directory":
				if err := os.Remove(artifact); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(artifact, 0o700); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				file, err := os.OpenFile(artifact, os.O_WRONLY, 0o600) // #nosec G304 -- Fixed artifact name beneath a test-owned submission directory.
				if err != nil {
					t.Fatal(err)
				}
				err = file.Truncate((16 << 20) + 1)
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("size fixture: %v %v", err, closeErr)
				}
			}
			if _, err := f.submit(path); err == nil {
				t.Fatal("accepted unsafe artifact")
			}
			unchangedAuthority(t, f, before)
		})
	}
}

func TestRelayFreshness(t *testing.T) {
	for name, change := range map[string]func(*testing.T, *relayFixture){
		"input": func(t *testing.T, f *relayFixture) {
			relayWrite(t, filepath.Join(f.root, "input.txt"), []byte("changed"))
		},
		"command": func(t *testing.T, f *relayFixture) {
			relayWrite(t, filepath.Join(f.root, ".naru/config.yaml"), []byte(strings.Replace(relayConfig, "touch must-not-run", "touch different", 1)))
		},
		"timeout": func(t *testing.T, f *relayFixture) {
			relayWrite(t, filepath.Join(f.root, ".naru/config.yaml"), []byte(strings.Replace(relayConfig, "30s", "31s", 1)))
		},
		"requirements": func(t *testing.T, f *relayFixture) {
			relayWrite(t, filepath.Join(f.root, ".naru/config.yaml"), []byte(strings.Replace(relayConfig, "naru-relay-missing-tool", "another-missing-tool", 1)))
		},
		"excludes": func(t *testing.T, f *relayFixture) {
			relayWrite(t, filepath.Join(f.root, ".naru/config.yaml"), []byte(strings.Replace(relayConfig, "ignored.txt", "other.txt", 1)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := freshRelay(t)
			path, _ := f.submission(t)
			before := f.authority(t)
			change(t, f)
			if _, err := f.submit(path); err == nil {
				t.Fatal("accepted stale request")
			}
			unchangedAuthority(t, f, before)
		})
	}
	t.Run("expiry boundary", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		before := f.authority(t)
		if _, err := relay.Submit(f.root, f.packet.PacketID, path, relay.SubmitOptions{Now: func() time.Time { return f.packet.ExpiresAt }}); err == nil {
			t.Fatal("accepted exact expiry")
		}
		unchangedAuthority(t, f, before)
	})
	t.Run("change after artifact copy preserves pending and isolates retry", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		before := f.authority(t)
		input := filepath.Join(f.root, "input.txt")
		original := relayRead(t, input)
		attemptPattern := filepath.Join(f.root, ".naru/relay/artifacts", f.packet.PacketID, "*", "execution.log")
		var abandoned string
		// The injected clock observes publication, without depending on how many
		// earlier time checks Submit makes. Change input only after bytes exist.
		_, err := relay.Submit(f.root, f.packet.PacketID, path, relay.SubmitOptions{Now: func() time.Time {
			attempts, err := filepath.Glob(attemptPattern)
			if err != nil {
				t.Fatal(err)
			}
			if abandoned == "" && len(attempts) == 1 {
				abandoned = attempts[0]
				relayWrite(t, input, []byte("changed after artifact publication"))
			}
			return f.now.Add(3 * time.Second)
		}})
		if abandoned == "" {
			t.Fatal("fixture never observed artifact publication")
		}
		if !errors.Is(err, relay.ErrNotReady) {
			t.Fatalf("input mutation after copying was not rejected: %v", err)
		}
		unchangedAuthority(t, f, before)
		abandonedBytes := relayRead(t, abandoned)
		relayWrite(t, input, original)
		r, err := f.submit(path)
		if err != nil {
			t.Fatal(err)
		}
		if r.Acceptance == nil || r.Acceptance.Result != "pass" || len(r.Acceptance.Artifacts) != 1 {
			t.Fatalf("retry did not accept complete evidence: %+v", r)
		}
		accepted := filepath.Join(f.root, filepath.FromSlash(r.Acceptance.Artifacts[0].StoredPath))
		if accepted == abandoned || filepath.Dir(accepted) == filepath.Dir(abandoned) {
			t.Fatal("retry reused the abandoned attempt")
		}
		attempts, err := filepath.Glob(attemptPattern)
		if err != nil || len(attempts) != 2 {
			t.Fatalf("expected distinct abandoned and accepted attempts: %v %v", attempts, err)
		}
		if !bytes.Equal(abandonedBytes, relayRead(t, abandoned)) {
			t.Fatal("retry overwrote abandoned immutable bytes")
		}
		if err := receipt.ValidateRelayEvidence(f.root, r, f.now.Add(4*time.Second)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("superseded", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		if _, err := relay.New(f.root, "remote", f.options()); err != nil {
			t.Fatal(err)
		}
		before := f.authority(t)
		if _, err := f.submit(path); err == nil {
			t.Fatal("accepted superseded request")
		}
		unchangedAuthority(t, f, before)
	})
	t.Run("replay", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		if _, err := f.submit(path); err != nil {
			t.Fatal(err)
		}
		before := f.authority(t)
		if _, err := f.submit(path); err == nil {
			t.Fatal("accepted replay")
		}
		unchangedAuthority(t, f, before)
	})
	t.Run("lock and cancelled submit", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		before := f.authority(t)
		lock, err := runlock.Acquire(f.root, "remote")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.submit(path); err == nil {
			t.Error("submitted while local owner holds lock")
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		unchangedAuthority(t, f, before)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := relay.Submit(f.root, f.packet.PacketID, path, relay.SubmitOptions{Context: ctx, Now: func() time.Time { return f.now.Add(3 * time.Second) }}); err == nil {
			t.Fatal("accepted cancelled submission")
		}
		unchangedAuthority(t, f, before)
	})
	t.Run("concurrent submission has one winner", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		start := make(chan struct{})
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() { <-start; _, err := f.submit(path); results <- err }()
		}
		close(start)
		successes := 0
		for i := 0; i < 2; i++ {
			if <-results == nil {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("accepted %d submissions", successes)
		}
		r, err := receipt.DecodeRelayRecord(f.authority(t), "remote")
		if err != nil || r.Acceptance == nil {
			t.Fatalf("authority: %+v %v", r, err)
		}
		if err := receipt.ValidateRelayEvidence(f.root, *r, f.now.Add(4*time.Second)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("corrupt authority cannot disappear", func(t *testing.T) {
		f := freshRelay(t)
		path, _ := f.submission(t)
		relayWrite(t, filepath.Join(f.root, ".naru/state/checks/remote.json"), []byte(`{"schema":999}`))
		before := f.authority(t)
		if _, err := f.submit(path); err == nil {
			t.Fatal("submitted over unsupported authority")
		}
		if _, err := relay.New(f.root, "remote", f.options()); err == nil {
			t.Fatal("issued over unsupported authority")
		}
		unchangedAuthority(t, f, before)
	})
}
