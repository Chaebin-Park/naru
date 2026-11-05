package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/runlock"
	"github.com/Chaebin-Park/naru/internal/runner"
	"github.com/Chaebin-Park/naru/internal/surface"
)

const policyTestConfig = `version: 1
default_mode: m
modes: {m: [c]}
checks: {c: {run: 'true', inputs: [src/**]}}
surfaces:
  policy-findings:
    run: sh tools/adapter.sh
    inputs: [findings.json, mode.txt, src/**]
    extractor_version: v1
    timeout: 2s
`
const policyTestIndex = `version: 1
surface: policy-findings
definition_files: [tools/adapter.sh]
sources: {DEC-001: docs/no-debug-api.md}
rules:
  no-debug-api:
    tool: api-check
    tool_config: tools/rules.json
    source: DEC-001
    fixtures:
      - {project: testdata/pass, expect: pass}
      - {project: testdata/fail, expect: violation}
`
const policyTestAdapter = `case "$(cat mode.txt)" in
  fail) exit 7 ;;
  hold) printf started > started; while :; do :; done ;;
  partial) printf '{"schema":1,'; exit 0 ;;
esac
cat findings.json
`

func policyWrite(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	} // #nosec G703 -- Test-owned temporary root and fixed fixture-relative path.
}
func policyRead(t *testing.T, root, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 -- Test-owned temporary root and fixed fixture-relative path.
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func policyJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func policyFinding(t *testing.T, n int) map[string]any {
	t.Helper()
	location := fmt.Sprintf("src/routes.go#Route%d", n)
	fingerprint := fmt.Sprintf("%064x", n+1)
	sum := sha256.Sum256(policyJSON(t, []string{"no-debug-api", location, fingerprint}))
	return map[string]any{"id": "finding:" + hex.EncodeToString(sum[:]), "value": map[string]any{"kind": "finding", "rule_id": "no-debug-api", "stable_location": location, "fingerprint": fingerprint, "file": "src/routes.go", "line": n + 1, "message": "Debug route is exposed"}}
}
func policyOutput(t *testing.T, count int) []byte {
	t.Helper()
	items := []any{map[string]any{"id": "coverage:no-debug-api", "value": map[string]any{"kind": "coverage", "rule_id": "no-debug-api", "complete": true}}}
	for i := 0; i < count; i++ {
		items = append(items, policyFinding(t, i))
	}
	return policyJSON(t, map[string]any{"schema": 1, "items": items})
}
func policyProject(t *testing.T, count int) string {
	t.Helper()
	root := t.TempDir()
	for rel, s := range map[string]string{".naru/config.yaml": policyTestConfig, ".naru/policy.yml": policyTestIndex, "tools/adapter.sh": policyTestAdapter, "tools/rules.json": "{}", "docs/no-debug-api.md": "# No debug API\n", "src/routes.go": "package fixture\n", "mode.txt": "normal"} {
		policyWrite(t, root, rel, []byte(s))
	}
	policyWrite(t, root, "findings.json", policyOutput(t, count))
	for name, n := range map[string]int{"pass": 0, "fail": 1} {
		prefix := "testdata/" + name + "/"
		for rel, s := range map[string]string{".naru/config.yaml": policyTestConfig, "tools/adapter.sh": policyTestAdapter, "tools/rules.json": "{}", "src/routes.go": "package fixture\n", "mode.txt": "normal"} {
			policyWrite(t, root, prefix+rel, []byte(s))
		}
		policyWrite(t, root, prefix+"findings.json", policyOutput(t, n))
	}
	return root
}
func policyCheck(t *testing.T, root string) *Report {
	t.Helper()
	r, e := Check(root, context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if r == nil {
		t.Fatal("missing report")
	}
	return r
}
func policyBootstrap(t *testing.T, root string) *Record {
	t.Helper()
	r := policyCheck(t, root)
	if r.State != "bootstrap-required" {
		t.Fatalf("state %q", r.State)
	}
	preview, e := Preview(root, "bootstrap")
	if e != nil {
		t.Fatal(e)
	}
	result, e := Approve(root, *preview, "test reviewer", "initial adoption", context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return result
}
func policyUnchanged(t *testing.T, root string, want []byte) {
	t.Helper()
	if !bytes.Equal(policyRead(t, root, ".naru/policy-baseline.json"), want) {
		t.Fatal("approved baseline bytes changed")
	}
}

func TestPolicyIndexValidation(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		root := policyProject(t, 1)
		idx, e := Load(root)
		if e != nil || idx.Surface != "policy-findings" || len(idx.Rules) != 1 || len(idx.DefinitionDigest) != 64 {
			t.Fatalf("index %+v: %v", idx, e)
		}
	})
	for _, tc := range []struct{ name, old, next string }{
		{"unknown_field", "version: 1", "version: 1\nunknown: true"},
		{"duplicate_field", "version: 1", "version: 1\nversion: 1"},
		{"null_version", "version: 1", "version: null"},
		{"unsupported_version", "version: 1", "version: 2"},
		{"unresolved_source", "source: DEC-001", "source: MISSING"},
		{"unknown_surface", "surface: policy-findings", "surface: missing"},
		{"unsafe_definition", "definition_files: [tools/adapter.sh]", "definition_files: [../adapter.sh]"},
		{"absolute_source", "docs/no-debug-api.md", "/tmp/no-debug-api.md"},
		{"duplicate_fixture", "project: testdata/fail", "project: testdata/pass"},
		{"missing_violation_case", "expect: violation", "expect: pass"},
		{"missing_rules", "rules:", "rules: {}\nunused:"},
		{"unknown_rule_field", "tool: api-check", "tool: api-check\n    unsafe: true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := policyProject(t, 1)
			policyWrite(t, root, ".naru/policy.yml", []byte(strings.Replace(policyTestIndex, tc.old, tc.next, 1)))
			if _, e := Load(root); !errors.Is(e, ErrInvalid) {
				t.Fatalf("accepted invalid index: %v", e)
			}
		})
	}
	for _, rel := range []string{"tools/rules.json", "docs/no-debug-api.md", "tools/adapter.sh"} {
		t.Run("missing_"+rel, func(t *testing.T) {
			root := policyProject(t, 1)
			if e := os.Remove(filepath.Join(root, rel)); e != nil {
				t.Fatal(e)
			}
			if _, e := Load(root); !errors.Is(e, ErrInvalid) {
				t.Fatalf("accepted missing reference: %v", e)
			}
		})
	}
	t.Run("symlink_definition", func(t *testing.T) {
		root := policyProject(t, 1)
		path := filepath.Join(root, "tools/rules.json")
		if e := os.Remove(path); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(filepath.Join(root, "docs/no-debug-api.md"), path); e != nil {
			t.Fatal(e)
		}
		if _, e := Load(root); !errors.Is(e, ErrInvalid) {
			t.Fatalf("accepted symlink: %v", e)
		}
	})
	t.Run("unbound_fixture", func(t *testing.T) {
		root := policyProject(t, 1)
		policyWrite(t, root, "testdata/pass/.naru/config.yaml", []byte(strings.Replace(policyTestConfig, "sh tools/adapter.sh", "cat findings.json", 1)))
		r, e := TestFixtures(root, "", context.Background())
		if e == nil && (r == nil || r.Passed) {
			t.Fatalf("unbound fixture accepted: %+v %v", r, e)
		}
	})
}

func TestPolicyFindingCompleteness(t *testing.T) {
	root := policyProject(t, 1)
	idx, e := Load(root)
	if e != nil {
		t.Fatal(e)
	}
	parse := func(t *testing.T, b []byte) ([]Finding, error) {
		t.Helper()
		s, e := surface.Build(surface.Request{Schema: 1, SurfaceID: "policy-findings", ExtractorVersion: "v1", InputDigest: strings.Repeat("1", 64)}, b, nil)
		if e != nil {
			return nil, e
		}
		return ParseFindings(s, idx)
	}
	t.Run("explicit_zero", func(t *testing.T) {
		f, e := parse(t, policyOutput(t, 0))
		if e != nil || len(f) != 0 {
			t.Fatalf("explicit zero %+v %v", f, e)
		}
	})
	t.Run("stable_identity", func(t *testing.T) {
		original, e := parse(t, policyOutput(t, 1))
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		if e = json.Unmarshal(policyOutput(t, 1), &v); e != nil {
			t.Fatal(e)
		}
		value := v["items"].([]any)[1].(map[string]any)["value"].(map[string]any)
		value["line"] = 999
		value["message"] = "Moved route"
		changed, e := parse(t, policyJSON(t, v))
		if e != nil || len(changed) != 1 || changed[0].ID != original[0].ID {
			t.Fatalf("presentation changed identity: %+v %v", changed, e)
		}
		if original[0].ID != FindingID("no-debug-api", "src/routes.go#Route0", fmt.Sprintf("%064x", 1)) {
			t.Fatal("identity differs from independently computed digest")
		}
	})
	for _, name := range []string{"missing_coverage", "false_coverage", "unknown_coverage", "duplicate_coverage", "unknown_rule", "wrong_identity", "fractional_line", "null_message", "unknown_finding_field", "duplicate_finding", "empty_items"} {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			if e := json.Unmarshal(policyOutput(t, 1), &v); e != nil {
				t.Fatal(e)
			}
			items := v["items"].([]any)
			coverage := items[0].(map[string]any)
			finding := items[1].(map[string]any)
			value := finding["value"].(map[string]any)
			switch name {
			case "missing_coverage":
				items = items[1:]
			case "false_coverage":
				coverage["value"].(map[string]any)["complete"] = false
			case "unknown_coverage":
				coverage["id"] = "coverage:unknown"
				coverage["value"].(map[string]any)["rule_id"] = "unknown"
			case "duplicate_coverage":
				items = append(items, coverage)
			case "unknown_rule":
				value["rule_id"] = "unknown"
			case "wrong_identity":
				finding["id"] = "finding:" + strings.Repeat("f", 64)
			case "fractional_line":
				value["line"] = 1.5
			case "null_message":
				value["message"] = nil
			case "unknown_finding_field":
				value["extra"] = true
			case "duplicate_finding":
				items = append(items, finding)
			case "empty_items":
				items = []any{}
			}
			v["items"] = items
			if _, e := parse(t, policyJSON(t, v)); e == nil {
				t.Fatal("invalid or incomplete findings accepted")
			}
		})
	}
}

func TestPolicyReductionLifecycle(t *testing.T) {
	root := policyProject(t, 10)
	record := policyBootstrap(t, root)
	if len(record.Findings) != 10 {
		t.Fatal("bootstrap lost findings")
	}
	original := policyRead(t, root, ".naru/policy-baseline.json")
	policyWrite(t, root, "findings.json", policyOutput(t, 7))
	r := policyCheck(t, root)
	if len(r.Known) != 7 || len(r.New) != 0 || len(r.Resolved) != 3 {
		t.Fatalf("10 to 7: %+v", r)
	}
	policyUnchanged(t, root, original)
	review, e := Preview(root, "reduction")
	if e != nil {
		t.Fatal(e)
	}
	record, e = Approve(root, *review, "test reviewer", "remove fixed routes", context.Background())
	if e != nil || len(record.Findings) != 7 {
		t.Fatalf("reduction %+v %v", record, e)
	}
	reduced := policyRead(t, root, ".naru/policy-baseline.json")
	if bytes.Equal(original, reduced) {
		t.Fatal("approved reduction did not publish")
	}
	policyWrite(t, root, "findings.json", policyOutput(t, 8))
	r = policyCheck(t, root)
	if len(r.New) != 1 || len(r.Known) != 7 || len(r.Resolved) != 0 {
		t.Fatalf("reintroduced finding not new: %+v", r)
	}
	if _, e := Preview(root, "reduction"); !errors.Is(e, ErrGrowth) {
		t.Fatalf("growth accepted: %v", e)
	}
	policyUnchanged(t, root, reduced)
	policyWrite(t, root, "findings.json", policyOutput(t, 7))
	policyCheck(t, root)
	review, e = Preview(root, "reduction")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Approve(root, *review, "test reviewer", "nothing changed", context.Background()); e != nil {
		t.Fatal(e)
	}
	policyUnchanged(t, root, reduced)
	policyWrite(t, root, "findings.json", policyOutput(t, 0))
	r = policyCheck(t, root)
	if len(r.Resolved) != 7 {
		t.Fatalf("empty reduction: %+v", r)
	}
	review, e = Preview(root, "reduction")
	if e != nil {
		t.Fatal(e)
	}
	record, e = Approve(root, *review, "test reviewer", "all routes fixed", context.Background())
	if e != nil || len(record.Findings) != 0 {
		t.Fatalf("empty approval %+v %v", record, e)
	}
	if _, e = os.Stat(filepath.Join(root, ".naru/baselines/policy-findings.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("policy wrote a contract baseline")
	}
}

func TestPolicyFreshness(t *testing.T) {
	t.Run("source_content_is_not_definition", func(t *testing.T) {
		root := policyProject(t, 2)
		policyBootstrap(t, root)
		before, e := Load(root)
		if e != nil {
			t.Fatal(e)
		}
		policyWrite(t, root, "src/routes.go", []byte("package fixture // fixed a route\n"))
		policyWrite(t, root, "findings.json", policyOutput(t, 1))
		r := policyCheck(t, root)
		if r.DefinitionDigest != before.DefinitionDigest || len(r.Resolved) != 1 {
			t.Fatalf("normal source edit incompatible: %+v", r)
		}
	})
	for _, rel := range []string{"tools/adapter.sh", "tools/rules.json", "docs/no-debug-api.md", ".naru/config.yaml"} {
		t.Run("definition_"+rel, func(t *testing.T) {
			root := policyProject(t, 2)
			policyBootstrap(t, root)
			original := policyRead(t, root, ".naru/policy-baseline.json")
			b := policyRead(t, root, rel)
			if rel == ".naru/config.yaml" {
				b = []byte(strings.Replace(string(b), "findings.json, mode.txt, src/**", "findings.json, mode.txt", 1))
			} else {
				b = append(b, '\n')
			}
			policyWrite(t, root, rel, b)
			if _, e := Check(root, context.Background()); !errors.Is(e, ErrIncompatible) {
				t.Fatalf("definition change accepted: %v", e)
			}
			policyUnchanged(t, root, original)
		})
	}
	for _, name := range []string{"source_edit", "candidate_revision", "source_failed", "baseline_revision", "index_change"} {
		t.Run(name, func(t *testing.T) {
			root := policyProject(t, 2)
			policyBootstrap(t, root)
			policyWrite(t, root, "findings.json", policyOutput(t, 1))
			policyCheck(t, root)
			review, e := Preview(root, "reduction")
			if e != nil {
				t.Fatal(e)
			}
			original := policyRead(t, root, ".naru/policy-baseline.json")
			switch name {
			case "source_edit":
				policyWrite(t, root, "src/routes.go", []byte("package changed\n"))
			case "candidate_revision":
				path := ".naru/state/policy/candidate.json"
				policyWrite(t, root, path, append(policyRead(t, root, path), '\n'))
			case "source_failed":
				e = (baseline.Store{Root: root}).Put(baseline.State{Schema: 1, SurfaceID: "policy-findings", Phase: "failed", Reason: "extractor_failed"})
				if e != nil {
					t.Fatal(e)
				}
			case "baseline_revision":
				original = append(original, '\n')
				policyWrite(t, root, ".naru/policy-baseline.json", original)
			case "index_change":
				policyWrite(t, root, ".naru/policy.yml", []byte(strings.Replace(policyTestIndex, "tool: api-check", "tool: another-check", 1)))
			}
			if _, e := Approve(root, *review, "test reviewer", "stale preview", context.Background()); e == nil {
				t.Fatal("changed preview approved")
			}
			policyUnchanged(t, root, original)
		})
	}
}

func TestPolicyPublicationFailure(t *testing.T) {
	t.Run("null_ready_candidate_reason", func(t *testing.T) {
		root := policyProject(t, 2)
		policyBootstrap(t, root)
		original := policyRead(t, root, ".naru/policy-baseline.json")
		policyWrite(t, root, "findings.json", policyOutput(t, 1))
		policyCheck(t, root)
		var envelope map[string]any
		if e := json.Unmarshal(policyRead(t, root, ".naru/state/policy/candidate.json"), &envelope); e != nil {
			t.Fatal(e)
		}
		envelope["reason"] = nil
		policyWrite(t, root, ".naru/state/policy/candidate.json", policyJSON(t, envelope))
		if _, e := Preview(root, "reduction"); e == nil {
			t.Fatal("null required candidate field accepted")
		}
		policyUnchanged(t, root, original)
	})

	t.Run("ordinary_check_lock_is_independent", func(t *testing.T) {
		root := policyProject(t, 1)
		lock, e := runlock.Acquire(root, "policy")
		if e != nil {
			t.Fatal(e)
		}
		defer func() {
			if e := lock.Close(); e != nil {
				t.Error(e)
			}
		}()
		report, e := Check(root, context.Background())
		if e != nil || report == nil || report.State != "bootstrap-required" {
			t.Fatalf("ordinary check lock blocked policy scan: %+v %v", report, e)
		}
	})

	t.Run("active_scan_cancellation", func(t *testing.T) {
		root := policyProject(t, 2)
		policyBootstrap(t, root)
		original := policyRead(t, root, ".naru/policy-baseline.json")
		policyWrite(t, root, "findings.json", policyOutput(t, 1))
		policyCheck(t, root)
		policyWrite(t, root, "mode.txt", []byte("hold"))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, e := Check(root, ctx); done <- e }()
		deadline := time.NewTimer(1500 * time.Millisecond)
		defer deadline.Stop()
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		started := false
		for !started {
			select {
			case <-deadline.C:
				t.Fatal("adapter did not start")
			case e := <-done:
				t.Fatalf("scan ended before cancellation: %v", e)
			case <-tick.C:
				if _, e := os.Stat(filepath.Join(root, "started")); e == nil {
					started = true
				}
			}
		}
		cancel()
		select {
		case e := <-done:
			if !errors.Is(e, runner.ErrInterrupted) {
				t.Fatalf("cancel result: %v", e)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("cancelled scan did not return")
		}
		if _, e := Preview(root, "reduction"); e == nil {
			t.Fatal("cancelled scan retained candidate authority")
		}
		policyUnchanged(t, root, original)
	})
	t.Run("forged_ready_candidate", func(t *testing.T) {
		root := policyProject(t, 2)
		policyBootstrap(t, root)
		original := policyRead(t, root, ".naru/policy-baseline.json")
		policyWrite(t, root, "findings.json", policyOutput(t, 1))
		policyCheck(t, root)
		var envelope map[string]any
		if e := json.Unmarshal(policyRead(t, root, ".naru/state/policy/candidate.json"), &envelope); e != nil {
			t.Fatal(e)
		}
		envelope["findings"] = []any{}
		policyWrite(t, root, ".naru/state/policy/candidate.json", policyJSON(t, envelope))
		if _, e := Preview(root, "reduction"); e == nil {
			t.Fatal("candidate findings not bound to source snapshot")
		}
		policyUnchanged(t, root, original)
	})
	for _, name := range []string{"unapproved", "null_findings", "duplicate_finding", "missing_approval_field", "unknown_approval_field"} {
		t.Run("corrupt_record_"+name, func(t *testing.T) {
			root := policyProject(t, 2)
			policyBootstrap(t, root)
			var envelope map[string]any
			if e := json.Unmarshal(policyRead(t, root, ".naru/policy-baseline.json"), &envelope); e != nil {
				t.Fatal(e)
			}
			switch name {
			case "unapproved":
				envelope["approval"] = nil
			case "null_findings":
				envelope["findings"] = nil
			case "duplicate_finding":
				rows := envelope["findings"].([]any)
				envelope["findings"] = append(rows, rows[0])
			case "missing_approval_field":
				delete(envelope["approval"].(map[string]any), "previous_revision")
			case "unknown_approval_field":
				envelope["approval"].(map[string]any)["force"] = true
			}
			corrupt := policyJSON(t, envelope)
			policyWrite(t, root, ".naru/policy-baseline.json", corrupt)
			if _, e := Check(root, context.Background()); e == nil {
				t.Fatal("corrupt approved record accepted")
			}
			if _, e := Preview(root, "bootstrap"); e == nil {
				t.Fatal("corrupt record replaced by bootstrap")
			}
			policyUnchanged(t, root, corrupt)
		})
	}

	for _, mode := range []string{"fail", "partial", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			root := policyProject(t, 2)
			policyBootstrap(t, root)
			original := policyRead(t, root, ".naru/policy-baseline.json")
			policyWrite(t, root, "findings.json", policyOutput(t, 1))
			policyCheck(t, root)
			if mode == "incomplete" {
				policyWrite(t, root, "findings.json", []byte(`{"schema":1,"items":[]}`))
			} else {
				policyWrite(t, root, "mode.txt", []byte(mode))
			}
			if _, e := Check(root, context.Background()); e == nil {
				t.Fatal("failed scan accepted")
			}
			if _, e := Preview(root, "reduction"); e == nil {
				t.Fatal("failed scan retained ready authority")
			}
			policyUnchanged(t, root, original)
		})
	}
	t.Run("cancelled_approval", func(t *testing.T) {
		root := policyProject(t, 2)
		policyBootstrap(t, root)
		original := policyRead(t, root, ".naru/policy-baseline.json")
		policyWrite(t, root, "findings.json", policyOutput(t, 1))
		policyCheck(t, root)
		review, e := Preview(root, "reduction")
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, e := Approve(root, *review, "test reviewer", "cancelled", ctx); !errors.Is(e, context.Canceled) {
			t.Fatalf("cancel result: %v", e)
		}
		policyUnchanged(t, root, original)
	})
	t.Run("lock", func(t *testing.T) {
		root := policyProject(t, 2)
		policyBootstrap(t, root)
		original := policyRead(t, root, ".naru/policy-baseline.json")
		policyWrite(t, root, "findings.json", policyOutput(t, 1))
		policyCheck(t, root)
		review, e := Preview(root, "reduction")
		if e != nil {
			t.Fatal(e)
		}
		lock, e := acquirePolicy(root)
		if e != nil {
			t.Fatal(e)
		}
		defer func() {
			if e := lock.Close(); e != nil {
				t.Error(e)
			}
		}()
		if _, e := Check(root, context.Background()); !errors.Is(e, runlock.ErrBusy) {
			t.Fatalf("concurrent scan: %v", e)
		}
		if _, e := Approve(root, *review, "test reviewer", "overlap", context.Background()); !errors.Is(e, runlock.ErrBusy) {
			t.Fatalf("concurrent approval: %v", e)
		}
		policyUnchanged(t, root, original)
	})
	t.Run("rename_failure", func(t *testing.T) {
		root := policyProject(t, 2)
		policyBootstrap(t, root)
		original := policyRead(t, root, ".naru/policy-baseline.json")
		sentinel := errors.New("injected rename failure")
		e := publish(filepath.Join(root, ".naru/policy-baseline.json"), []byte("replacement"), func(string, string) error { return sentinel })
		if !errors.Is(e, sentinel) {
			t.Fatalf("rename result: %v", e)
		}
		policyUnchanged(t, root, original)
	})
	for _, bad := range []string{"", `{}`, `{"schema":1,`, `null`} {
		t.Run("malformed_baseline_"+fmt.Sprint(len(bad)), func(t *testing.T) {
			root := policyProject(t, 1)
			policyWrite(t, root, ".naru/policy-baseline.json", []byte(bad))
			if _, e := Check(root, context.Background()); e == nil {
				t.Fatal("malformed baseline treated as absence")
			}
			if _, e := Preview(root, "bootstrap"); e == nil {
				t.Fatal("malformed baseline bootstrapped")
			}
			policyUnchanged(t, root, []byte(bad))
		})
	}
}
