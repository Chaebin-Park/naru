package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/policy"
)

const policyFixtureConfig = `version: 1
default_mode: m
modes: {m: [c]}
checks: {c: {run: 'true', inputs: [findings.json]}}
surfaces:
  findings:
    run: sh adapter.sh
    inputs: [findings.json]
    extractor_version: v1
`
const policyIndexFixture = `version: 1
surface: findings
definition_files: [adapter.sh]
sources: {DEC-1: rationale.md}
rules:
  no-debug:
    tool: fixture-adapter
    tool_config: rules.json
    source: DEC-1
    fixtures:
      - {project: fixtures/pass, expect: pass}
      - {project: fixtures/fail, expect: violation}
`

func policyFixtureOutput(t *testing.T, violation bool) string {
	t.Helper()
	items := []any{map[string]any{"id": "coverage:no-debug", "value": map[string]any{"kind": "coverage", "rule_id": "no-debug", "complete": true}}}
	if violation {
		fingerprint := strings.Repeat("a", 64)
		items = append(items, map[string]any{"id": policy.FindingID("no-debug", "src/routes.go#Debug", fingerprint), "value": map[string]any{"kind": "finding", "rule_id": "no-debug", "stable_location": "src/routes.go#Debug", "fingerprint": fingerprint, "file": "src/routes.go", "line": 4, "message": "Debug route exposed"}})
	}
	data, err := json.Marshal(map[string]any{"schema": 1, "items": items})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func newPolicyProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, ".naru/config.yaml", policyFixtureConfig)
	write(t, root, ".naru/policy.yml", policyIndexFixture)
	write(t, root, "adapter.sh", "cat findings.json\n")
	write(t, root, "rules.json", "{}\n")
	write(t, root, "rationale.md", "# No debug routes\n")
	write(t, root, "findings.json", policyFixtureOutput(t, true))
	for _, kind := range []string{"pass", "fail"} {
		prefix := "fixtures/" + kind + "/"
		write(t, root, prefix+".naru/config.yaml", policyFixtureConfig)
		write(t, root, prefix+"adapter.sh", "cat findings.json\n")
		write(t, root, prefix+"rules.json", "{}\n")
		write(t, root, prefix+"findings.json", policyFixtureOutput(t, kind == "fail"))
	}
	return root
}
func policyReview(t *testing.T, root, kind, confirmation string) (*policy.Record, error) {
	t.Helper()
	var output bytes.Buffer
	return reviewPolicy(context.Background(), root, kind, Env{Stdin: strings.NewReader("Reviewer\nReviewed fixtures\n" + confirmation + "\n"), Stderr: &output})
}
func TestPolicyCLI(t *testing.T) {
	root := newPolicyProject(t)
	for _, args := range [][]string{{"policy"}, {"policy", "unknown"}, {"policy", "list", "extra"}, {"policy", "check", "--unknown"}} {
		if got := run(t, root, "", args...); got.code != 64 {
			t.Fatalf("usage %+v", got)
		}
	}
	if got := run(t, root, "", "policy", "list", "--json"); got.code != 0 || !strings.Contains(got.stdout, "no-debug") {
		t.Fatal(got)
	}
	if got := run(t, root, "", "policy", "list"); got.code != 0 || !strings.Contains(got.stdout, "tool=fixture-adapter") {
		t.Fatal(got)
	}
	if got := run(t, root, "", "policy", "check", "--json"); got.code != 1 || !strings.Contains(got.stdout, "bootstrap-required") {
		t.Fatal(got)
	}
	for _, action := range []string{"bootstrap", "approve"} {
		if got := run(t, root, "Reviewer\nReason\n", "policy", action); got.code != 64 || !strings.Contains(got.stderr, "requires terminal") {
			t.Fatal(got)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".naru/policy-baseline.json")); !os.IsNotExist(err) {
		t.Fatal("nonterminal mutated baseline")
	}
	preview, err := policy.Preview(root, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyReview(t, root, "bootstrap", "wrong"); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	if _, err := policyReview(t, root, "bootstrap", "bootstrap "+preview.Report.CandidateDigest); err != nil {
		t.Fatal(err)
	}
	if got := run(t, root, "", "policy", "check"); got.code != 0 || !strings.Contains(got.stdout, "known=1") || !strings.Contains(got.stderr, "known policy") {
		t.Fatal(got)
	}
	original, err := os.ReadFile(filepath.Join(root, ".naru/policy-baseline.json")) // #nosec G304 -- Fixed baseline in a test-owned temporary project.
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "findings.json", policyFixtureOutput(t, false))
	if got := run(t, root, "", "policy", "check"); got.code != 0 || !strings.Contains(got.stdout, "resolved=1") {
		t.Fatal(got)
	}
	before, err := os.ReadFile(filepath.Join(root, ".naru/policy-baseline.json")) // #nosec G304 -- Fixed baseline in a test-owned temporary project.
	if err != nil || !bytes.Equal(before, original) {
		t.Fatal("check shrank baseline automatically", err)
	}
	preview, err = policy.Preview(root, "reduction")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyReview(t, root, "reduction", "reduction "+preview.Report.CandidateDigest); err == nil {
		t.Fatal("empty reduction did not require explicit confirmation")
	}
	if _, err := policyReview(t, root, "reduction", "reduction empty "+preview.Report.CandidateDigest); err != nil {
		t.Fatal(err)
	}
	if got := run(t, root, "", "policy", "check"); got.code != 0 {
		t.Fatal(got)
	}
	reduced, err := os.ReadFile(filepath.Join(root, ".naru/policy-baseline.json")) // #nosec G304 -- Fixed test-owned policy baseline.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyReview(t, root, "reduction", ""); err != nil {
		t.Fatal(err)
	}
	unchanged, err := os.ReadFile(filepath.Join(root, ".naru/policy-baseline.json")) // #nosec G304 -- Fixed test-owned policy baseline.
	if err != nil || !bytes.Equal(reduced, unchanged) {
		t.Fatal("no-op rewrote approval", err)
	}
	write(t, root, "findings.json", policyFixtureOutput(t, true))
	if got := run(t, root, "", "policy", "check", "--json"); got.code != 1 || !strings.Contains(got.stdout, `"state":"new"`) {
		t.Fatal(got)
	}
	if got := run(t, root, "", "check", "c"); got.code != 0 {
		t.Fatal(got)
	}
	if got := run(t, root, "", "status"); got.code != 0 {
		t.Fatalf("policy implicitly changed ordinary check status: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".naru/baselines/findings.json")); !os.IsNotExist(err) {
		t.Fatal("policy approval wrote contract baseline")
	}
	write(t, root, "rules.json", "{\"changed\":true}\n")
	if got := run(t, root, "", "policy", "check"); got.code != 1 {
		t.Fatalf("incompatible definition: %+v", got)
	}
	write(t, root, ".naru/policy.yml", strings.Replace(policyIndexFixture, "source: DEC-1", "source: missing", 1))
	if got := run(t, root, "", "policy", "list"); got.code != 64 {
		t.Fatalf("invalid index: %+v", got)
	}
}

func TestPolicyFixtures(t *testing.T) {
	t.Run("bound_pass_and_violation", func(t *testing.T) {
		root := newPolicyProject(t)
		got := run(t, root, "", "policy", "test", "no-debug", "--json")
		var report policy.FixtureReport
		if got.code != 0 || json.Unmarshal([]byte(got.stdout), &report) != nil || !report.Passed || len(report.Results) != 2 {
			t.Fatalf("fixtures %+v", got)
		}
		for _, kind := range []string{"pass", "fail"} {
			if _, err := os.Stat(filepath.Join(root, "fixtures", kind, ".naru/state")); !os.IsNotExist(err) {
				t.Fatal("fixture source mutated")
			}
		}
		if got := run(t, root, "", "policy", "test", "unknown"); got.code != 64 {
			t.Fatal(got)
		}
	})
	for _, fault := range []string{"adapter", "tool_config", "surface", "incomplete", "unavailable", "symlink"} {
		t.Run(fault, func(t *testing.T) {
			root := newPolicyProject(t)
			switch fault {
			case "adapter":
				write(t, root, "fixtures/pass/adapter.sh", "echo fake\n")
			case "tool_config":
				write(t, root, "fixtures/pass/rules.json", "{\"fake\":true}\n")
			case "surface":
				write(t, root, "fixtures/pass/.naru/config.yaml", strings.Replace(policyFixtureConfig, "sh adapter.sh", "true", 1))
			case "incomplete":
				write(t, root, "fixtures/pass/findings.json", `{"schema":1,"items":[]}`)
			case "unavailable":
				cfg := policyFixtureConfig + "    requires: {tools: [naru-no-such-policy-fixture-tool]}\n"
				write(t, root, ".naru/config.yaml", cfg)
				for _, kind := range []string{"pass", "fail"} {
					write(t, root, fmt.Sprintf("fixtures/%s/.naru/config.yaml", kind), cfg)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(root, "rules.json"), filepath.Join(root, "fixtures/pass/link")); err != nil {
					t.Fatal(err)
				}
			}
			if got := run(t, root, "", "policy", "test", "--json"); got.code != 1 {
				t.Fatalf("%s falsely passed: %+v", fault, got)
			}
		})
	}
}
