package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/status"
)

func TestSurfaceDecisionIntegration(t *testing.T) {
	root := newProject(t, false)
	cfg := projectConfig + "surfaces: {api: {run: 'cat output.json', inputs: [output.json], extractor_version: v1}}\nmode_surfaces: {code: [api]}\n"
	write(t, root, ".naru/config.yaml", cfg)
	write(t, root, "src/value.txt", "ok\n")
	write(t, root, "README.md", "docs\n")
	write(t, root, "output.json", `{"schema":1,"items":[{"id":"route","value":{"method":"GET"}}]}`)
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	check := func(want string, pass bool) {
		t.Helper()
		r := run(t, root, "", "status", "--json")
		var rep status.Report
		if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
			t.Fatalf("%+v: %v", r, err)
		}
		if rep.Schema != 3 || rep.Satisfied != pass || len(rep.Surfaces) != 1 || rep.Surfaces[0].State != want {
			t.Fatalf("report: %+v", rep)
		}
		expectedStatus, expectedStop := 1, 2
		if pass {
			expectedStatus, expectedStop = 0, 0
		}
		if r.code != expectedStatus {
			t.Fatal(r)
		}
		if r := run(t, root, stopPayload(root), "gate", "stop"); r.code != expectedStop {
			t.Fatalf("stop: %+v", r)
		}
		if r := run(t, root, "", "gate", "pre-push"); r.code != expectedStatus {
			t.Fatalf("push: %+v", r)
		}
		if r := run(t, root, "", "status", "--markdown"); r.code != expectedStatus || !strings.Contains(r.stdout, "| api | "+want+" |") {
			t.Fatalf("markdown: %+v", r)
		}
	}
	check("not-ready", false)
	if r := run(t, root, "", "baseline", "extract", "api"); r.code != 0 {
		t.Fatal(r)
	}
	check("missing-baseline", false)
	current, err := (baseline.Store{Root: root}).Current("api")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(current.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".naru/baselines/api.json", string(legacy))
	check("approval-required", false)
	approve := func() {
		t.Helper()
		preview, err := baseline.Diff(root, "api")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := baseline.Approve(context.Background(), root, "api", *preview, "test reviewer", "reviewed fixture"); err != nil {
			t.Fatal(err)
		}
	}
	approve()
	check("unchanged", true)
	lock, err := baseline.Acquire(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	check("running", false)
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	write(t, root, "output.json", `{"schema":1,"items":[{"id":"route","value":{"method":"POST"}}]}`)
	check("stale", false)
	if r := run(t, root, "", "baseline", "extract", "api"); r.code != 0 {
		t.Fatal(r)
	}
	check("changed", false)
	if r := run(t, root, "", "check", "--mode", "docs"); r.code != 0 {
		t.Fatal(r)
	}
	if r := run(t, root, "", "mode", "docs"); r.code != 0 {
		t.Fatal(r)
	}
	if r := run(t, root, stopPayload(root), "gate", "stop"); r.code != 0 {
		t.Fatalf("narrow stop: %+v", r)
	}
	if r := run(t, root, "", "gate", "pre-push"); r.code != 1 {
		t.Fatalf("narrow push: %+v", r)
	}
	if r := run(t, root, "", "mode", "code"); r.code != 0 {
		t.Fatal(r)
	}
	approve()
	check("unchanged", true)
	write(t, root, ".naru/baselines/api.json", "{broken")
	if r := run(t, root, "", "status", "--json"); r.code == 0 {
		t.Fatal("invalid approval passed")
	}
}
