package cli

import (
	"strings"
	"testing"
)

func TestUnavailableRequirementsCLI(t *testing.T) {
	root := newProject(t, false)
	write(t, root, ".naru/config.yaml", strings.Replace(projectConfig, "    run: grep", "    requires: {tools: [naru-missing-fixture-tool]}\n    run: grep", 1))
	got := run(t, root, "", "check")
	if got.code != 1 || !strings.Contains(got.stdout+got.stderr, "NOT-RUN") || !strings.Contains(got.stdout+got.stderr, "naru-missing-fixture-tool") {
		t.Fatalf("check: %+v", got)
	}
	got = run(t, root, "", "status", "--json")
	if got.code != 1 || !strings.Contains(got.stdout, `"state": "not-run"`) || !strings.Contains(got.stdout, `"reason": "missing_tool"`) || !strings.Contains(got.stdout, `"exit_code": null`) {
		t.Fatalf("status: %+v", got)
	}
	write(t, root, ".naru/config.yaml", strings.Replace(projectConfig, "    run: grep", "    requires: {tools: ['../tool']}\n    run: grep", 1))
	if got := run(t, root, "", "check"); got.code != 64 {
		t.Fatalf("invalid requirements: %+v", got)
	}
}
