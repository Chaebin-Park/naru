package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/policy"
	"github.com/Chaebin-Park/naru/internal/status"
)

func TestCommonReportIntegration(t *testing.T) {
	root := newPolicyProject(t)
	cfgText := strings.ReplaceAll(policyFixtureConfig, "[c]", "[has-ok]")
	cfgText = strings.Replace(cfgText, "checks: {c:", "checks: {has-ok:", 1)
	cfgText += "  api: {run: 'cat api.json', inputs: [api.json], extractor_version: v1}\nmode_surfaces: {m: [api]}\nmode_policy: {m: true}\ngate: {max_repeats: 1}\n"
	write(t, root, ".naru/config.yaml", cfgText)
	write(t, root, "api.json", `{"schema":1,"items":[]}`)
	packet := relayPacket(t, root)
	submission := relaySubmission(t, root, packet, true)
	if r := run(t, root, "", "relay", "submit", packet.PacketID, "--file", submission); r.code != 0 {
		t.Fatal(r)
	}
	if _, err := policy.Check(root, context.Background()); err != nil {
		t.Fatal(err)
	}
	preview, err := policy.Preview(root, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Approve(root, *preview, "reviewer", "reviewed", context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := run(t, root, "", "status", "--json"); r.code != 1 || !strings.Contains(r.stdout, "human-relayed") {
		t.Fatal(r)
	}
	// Valid relay and policy cannot hide missing surface approval. D13 may allow
	// an explicitly unverified stop, but must leave pre-push blocked.
	if r := run(t, root, stopPayload(root), "gate", "stop"); r.code != 2 {
		t.Fatal(r)
	}
	if r := run(t, root, `{"stop_hook_active":true}`, "gate", "stop"); r.code != 0 || !strings.Contains(r.stdout+r.stderr, "NOT VERIFIED") {
		t.Fatal(r)
	}
	if r := run(t, root, "", "gate", "pre-push"); r.code != 1 {
		t.Fatal(r)
	}
	if r := run(t, root, "", "baseline", "extract", "api"); r.code != 0 {
		t.Fatal(r)
	}
	diff, err := baseline.Diff(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := baseline.Approve(context.Background(), root, "api", *diff, "reviewer", "reviewed empty contract"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Now().UTC()
	first, err := status.EvaluateAt(root, cfg, "m", instant)
	if err != nil {
		t.Fatal(err)
	}
	second, err := status.EvaluateAt(root, cfg, "m", instant)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) || !first.Satisfied || !first.Unverified.Present || first.Schema != 3 {
		t.Fatalf("reports differ or invalid: %s / %s", a, b)
	}
	if r := run(t, root, "", "status", "--markdown"); r.code != 0 || !strings.Contains(r.stdout, "NOT VERIFIED") {
		t.Fatal(r)
	}
	if r := run(t, root, "", "gate", "pre-push"); r.code != 1 {
		t.Fatal(r)
	}
	if r := run(t, root, stopPayload(root), "gate", "stop"); r.code != 0 {
		t.Fatal(r)
	}
	if r := run(t, root, "", "gate", "pre-push"); r.code != 0 {
		t.Fatal(r)
	}
}
