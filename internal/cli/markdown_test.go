package cli

import (
	"strings"
	"testing"
)

func TestStatusMarkdown(t *testing.T) {
	root := newProject(t, false)
	r := run(t, root, "", "status", "--markdown")
	if r.code != 1 || !strings.Contains(r.stdout, "| has-ok | missing |") {
		t.Fatalf("missing: %+v", r)
	}
	write(t, root, "src/value.txt", "ok\n")
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	r = run(t, root, "", "status", "--markdown")
	if r.code != 0 || !strings.Contains(r.stdout, "**satisfied**") {
		t.Fatalf("pass: %+v", r)
	}
	write(t, root, ".naru/state/unverified.json", `{"reason":"test"}`)
	r = run(t, root, "", "status", "--markdown")
	if r.code != 0 || !strings.Contains(r.stdout, "NOT VERIFIED") {
		t.Fatalf("marker: %+v", r)
	}
	write(t, root, "src/value.txt", "changed\n")
	r = run(t, root, "", "status", "--markdown")
	if r.code != 1 || !strings.Contains(r.stdout, "| has-ok | stale |") {
		t.Fatalf("stale: %+v", r)
	}
	r = run(t, root, "", "status", "--markdown", "--json")
	if r.code != 64 {
		t.Fatalf("conflicting formats: %+v", r)
	}
}
