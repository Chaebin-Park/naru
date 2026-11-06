package status

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runner"
)

func relayWrite(t *testing.T, root, relative, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil { // #nosec G703 -- Fixed fixture paths under a test-owned temporary root.
		t.Fatal(err)
	}
}

func relayFixture(t *testing.T, unavailable bool) (string, *config.Config, receipt.RelayRecord) {
	t.Helper()
	root := t.TempDir()
	definition := "version: 1\ndefault_mode: m\nmodes: {m: [c]}\nchecks:\n  c:\n    run: 'printf executed > executed.txt; false'\n    inputs: [input.txt]\n"
	if unavailable {
		definition += "    requires: {tools: [naru-no-such-relay-fixture-tool]}\n"
	}
	relayWrite(t, root, ".naru/config.yaml", definition)
	relayWrite(t, root, "input.txt", "source")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	d, err := fileset.Compute(root, cfg.Checks["c"].Inputs, cfg.Exclude, cfg.Fingerprint("c"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	p := receipt.RelayPacket{Schema: 1, PacketID: strings.Repeat("a", 32), Check: "c", IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		InputDigest:    receipt.InputDigest{Hash: d.Hash, FileCount: d.FileCount, FileSource: string(d.Source)},
		DefinitionHash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(cfg.Fingerprint("c")))),
		Command:        cfg.Checks["c"].Run, Workdir: ".", TimeoutMS: cfg.Checks["c"].TimeoutDuration().Milliseconds(),
		Inputs: cfg.Checks["c"].Inputs, Excludes: append([]string{}, cfg.Exclude...),
		Requirements: receipt.RelayRequirements{Tools: append([]string{}, cfg.Checks["c"].Requires.Tools...), OS: []string{}},
		Target:       "test device", ExpectedArtifacts: []string{"execution.log"}}
	ph, err := receipt.PacketHash(p)
	if err != nil {
		t.Fatal(err)
	}
	artifact := ".naru/relay/artifacts/" + p.PacketID + "/" + strings.Repeat("b", 32) + "/execution.log"
	relayWrite(t, root, artifact, "private artifact bytes")
	zero := 0
	r := receipt.RelayRecord{Schema: receipt.RelaySchema, InputPolicy: receipt.RelayInputPolicy, Check: "c", RunID: p.PacketID,
		Phase: receipt.PhaseTerminal, Provenance: receipt.Provenance{Kind: receipt.EvidenceHumanRelayed}, Packet: p, PacketHash: ph,
		Acceptance: &receipt.RelayAcceptance{AcceptedAt: now.Add(3 * time.Second), SubmittedBy: "test reviewer", StartedAt: now.Add(time.Second), FinishedAt: now.Add(2 * time.Second),
			BeforeInputDigest: p.InputDigest, AfterInputDigest: p.InputDigest, Result: receipt.ResultPass, ExitCode: &zero,
			ToolVersions: []receipt.RelayToolVersion{{Name: "test-tool", Version: "1"}},
			Artifacts:    []receipt.RelayArtifact{{Name: "execution.log", StoredPath: artifact, Size: int64(len("private artifact bytes")), SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("private artifact bytes")))}}}}
	if err := receipt.NewStateStore(root).PutRelay(r); err != nil {
		t.Fatal(err)
	}
	return root, cfg, r
}

func TestRelayStatus(t *testing.T) {
	t.Run("valid_without_local_tools_and_private_output", func(t *testing.T) {
		root, cfg, r := relayFixture(t, true)
		rep, err := Evaluate(root, cfg, "m")
		if err != nil || !rep.Satisfied || rep.Checks[0].Provenance.Kind != receipt.EvidenceHumanRelayed || rep.Checks[0].Relay.PacketID != r.RunID {
			t.Fatalf("relay rejected: %+v %v", rep, err)
		}
		data, err := json.Marshal(rep)
		if err != nil {
			t.Fatal(err)
		}
		for _, hidden := range []string{"private artifact bytes", "execution.log", "submitted_by", "tool_versions"} {
			if strings.Contains(string(data), hidden) {
				t.Fatalf("status exposes private submission field %q", hidden)
			}
		}
	})
	for _, scenario := range []string{"pending", "failed", "input_changed", "definition_changed", "timeout_changed", "artifact_changed", "artifact_missing", "artifact_symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root, cfg, r := relayFixture(t, false)
			want := "relay_artifact_invalid"
			switch scenario {
			case "pending":
				r.Phase, r.Acceptance = receipt.PhasePending, nil
				want = "relay_pending"
			case "failed":
				code := 1
				r.Acceptance.Result, r.Acceptance.ExitCode = receipt.ResultFail, &code
				want = "relay_failed"
			case "input_changed":
				relayWrite(t, root, "input.txt", "changed")
				want = "inputs_changed"
			case "definition_changed":
				ch := cfg.Checks["c"]
				ch.Run = "true"
				cfg.Checks["c"] = ch
				want = "inputs_changed"
			case "timeout_changed":
				ch := cfg.Checks["c"]
				ch.Timeout = "1s"
				cfg.Checks["c"] = ch
				want = "inputs_changed"
			case "artifact_changed":
				relayWrite(t, root, r.Acceptance.Artifacts[0].StoredPath, "private artifact bytex")
			case "artifact_missing", "artifact_symlink":
				path := filepath.Join(root, filepath.FromSlash(r.Acceptance.Artifacts[0].StoredPath))
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if scenario == "artifact_symlink" {
					if err := os.Symlink(filepath.Join(root, "input.txt"), path); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := receipt.NewStateStore(root).PutRelay(r); err != nil {
				t.Fatal(err)
			}
			rep, err := Evaluate(root, cfg, "m")
			if err != nil || rep.Satisfied || rep.Checks[0].Reason != want {
				t.Fatalf("reason want %s: %+v %v", want, rep, err)
			}
		})
	}
	t.Run("time_boundaries", func(t *testing.T) {
		root, cfg, r := relayFixture(t, false)
		for _, tc := range []struct {
			now    time.Time
			reason string
		}{
			{r.Acceptance.AcceptedAt, "relay_verified"},
			{r.Acceptance.AcceptedAt.Add(-time.Nanosecond), "relay_clock_invalid"},
			{r.Packet.ExpiresAt.Add(-time.Nanosecond), "relay_verified"},
			{r.Packet.ExpiresAt, "relay_expired"},
		} {
			row, err := evaluateRelay(root, cfg, "c", r, tc.now)
			if err != nil || row.Reason != tc.reason {
				t.Fatalf("time %s: %+v %v", tc.now, row, err)
			}
		}
	})
	t.Run("other_checks_and_unverified_remain", func(t *testing.T) {
		root, cfg, _ := relayFixture(t, false)
		relayWrite(t, root, ".naru/state/unverified.json", `{"reason":"not_verified"}`)
		cfg.Checks["other"] = cfg.Checks["c"]
		cfg.Modes["m"] = append(cfg.Modes["m"], "other")
		rep, err := Evaluate(root, cfg, "m")
		if err != nil || rep.Satisfied || !rep.Unverified.Present || rep.Checks[0].State != Pass || rep.Checks[1].State != Missing {
			t.Fatalf("relay bypassed independent conditions: %+v %v", rep, err)
		}
	})
	for _, scenario := range []string{"ci_failure", "ci_unavailable", "local_replaces_pending"} {
		t.Run(scenario, func(t *testing.T) {
			root, cfg, r := relayFixture(t, scenario == "ci_unavailable")
			evidence := receipt.EvidenceCI
			if scenario == "local_replaces_pending" {
				r.Phase, r.Acceptance = receipt.PhasePending, nil
				if err := receipt.NewStateStore(root).PutRelay(r); err != nil {
					t.Fatal(err)
				}
				evidence = receipt.EvidenceLocal
			}
			out, err := runner.Run(root, cfg, "c", runner.Options{Evidence: evidence})
			if err != nil || out.Record == nil || out.Record.Provenance.Kind != evidence || *out.Record.Execution.Result == receipt.ResultPass {
				t.Fatalf("actual run did not replace relay: %+v %v", out, err)
			}
			if scenario != "ci_unavailable" {
				if _, err := os.Stat(filepath.Join(root, "executed.txt")); err != nil {
					t.Fatal("local command did not actually execute", err)
				}
			}
			rep, err := Evaluate(root, cfg, "m")
			if err != nil || rep.Satisfied || rep.Checks[0].Relay != nil || rep.Checks[0].Provenance.Kind != evidence {
				t.Fatalf("old relay used as fallback: %+v %v", rep, err)
			}
			paths, err := filepath.Glob(filepath.Join(root, ".naru/state/history/c/relay", r.RunID, "*.json"))
			if err != nil || len(paths) == 0 {
				t.Fatal("relay authority was not archived", paths, err)
			}
		})
	}
}

func TestReportEvaluationTime(t *testing.T) {
	root, cfg, record := relayFixture(t, true)
	cases := []struct {
		name   string
		at     time.Time
		state  State
		reason string
	}{
		{"before_acceptance", record.Acceptance.AcceptedAt.Add(-time.Nanosecond), Invalid, "relay_clock_invalid"},
		{"accepted", record.Acceptance.AcceptedAt, Pass, "relay_verified"},
		{"before_expiry", record.Packet.ExpiresAt.Add(-time.Nanosecond), Pass, "relay_verified"},
		{"expired", record.Packet.ExpiresAt, Stale, "relay_expired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 2; i++ {
				rep, err := EvaluateAt(root, cfg, "m", tc.at)
				if err != nil {
					t.Fatal(err)
				}
				if len(rep.Checks) != 1 || rep.Checks[0].State != tc.state || rep.Checks[0].Reason != tc.reason || rep.Satisfied != (tc.state == Pass) {
					t.Fatalf("unexpected report: %+v", rep)
				}
			}
		})
	}
}
