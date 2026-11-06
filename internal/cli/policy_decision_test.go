package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/policy"
	"github.com/Chaebin-Park/naru/internal/status"
)

func TestPolicyDecisionIntegration(t *testing.T) {
	root := newPolicyProject(t)
	write(t, root, ".naru/config.yaml", policyFixtureConfig+"mode_policy: {m: true}\n")
	refreshCheck := func() {
		t.Helper()
		if r := run(t, root, "", "check"); r.code != 0 {
			t.Fatal(r)
		}
	}
	refreshCheck()
	check := func(want string, pass bool) {
		t.Helper()
		r := run(t, root, "", "status", "--json")
		var rep status.Report
		if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
			t.Fatalf("%+v: %v", r, err)
		}
		if rep.Schema != 3 || rep.Satisfied != pass || rep.Policy == nil || rep.Policy.State != want {
			t.Fatalf("report: %+v", rep)
		}
		if strings.Contains(r.stdout, "Debug route exposed") {
			t.Fatal("private finding leaked")
		}
		expected, stop := 1, 2
		if pass {
			expected, stop = 0, 0
		}
		if r.code != expected {
			t.Fatal(r)
		}
		if r := run(t, root, stopPayload(root), "gate", "stop"); r.code != stop {
			t.Fatalf("stop: %+v", r)
		}
		if r := run(t, root, "", "gate", "pre-push"); r.code != expected {
			t.Fatalf("push: %+v", r)
		}
		if r := run(t, root, "", "status", "--markdown"); r.code != expected || !strings.Contains(r.stdout, "Policy: **"+want+"**") {
			t.Fatalf("markdown: %+v", r)
		}
	}
	check("not-ready", false)
	if r := run(t, root, "", "policy", "check"); r.code != 1 {
		t.Fatal(r)
	}
	check("bootstrap-required", false)
	approve := func(kind string) {
		t.Helper()
		preview, err := policy.Preview(root, kind)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := policy.Approve(root, *preview, "test reviewer", "reviewed fixture", context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	approve("bootstrap")
	check("known", true)
	write(t, root, "findings.json", policyFixtureOutput(t, false))
	refreshCheck()
	check("stale", false)
	if r := run(t, root, "", "policy", "check"); r.code != 0 {
		t.Fatal(r)
	}
	check("resolved", true)
	approve("reduction")
	check("clean", true)
	write(t, root, "findings.json", policyFixtureOutput(t, true))
	refreshCheck()
	if r := run(t, root, "", "policy", "check"); r.code != 1 {
		t.Fatal(r)
	}
	check("new", false)
	write(t, root, ".naru/policy-baseline.json", "{broken")
	if r := run(t, root, "", "status", "--json"); r.code == 0 {
		t.Fatal("malformed policy passed")
	}
}
