package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/receipt"
)

func relayPacket(t *testing.T, root string) receipt.RelayPacket {
	t.Helper()
	got := run(t, root, "", "relay", "new", "has-ok", "--target", "synthetic test machine", "--artifact", "execution.log", "--json")
	if got.code != 0 {
		t.Fatalf("issue: %+v", got)
	}
	var envelope struct {
		Packet     receipt.RelayPacket `json:"packet"`
		PacketHash string              `json:"packet_hash"`
		PacketPath string              `json:"packet_path"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	packet := envelope.Packet
	wantHash, err := receipt.PacketHash(packet)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.PacketHash != wantHash || envelope.PacketPath == "" {
		t.Fatalf("packet envelope: %+v", envelope)
	}
	if packet.PacketID == "" || packet.Check != "has-ok" {
		t.Fatalf("packet identity: %+v", packet)
	}
	return packet
}

func relaySubmission(t *testing.T, root string, packet receipt.RelayPacket, passed bool) string {
	t.Helper()
	packetHash, err := receipt.PacketHash(packet)
	if err != nil {
		t.Fatal(err)
	}
	content := "synthetic-private-artifact-output\n"
	write(t, root, "submitted/execution.log", content)
	result, exitCode := "pass", 0
	if !passed {
		result, exitCode = "fail", 1
	}
	submission := map[string]any{
		"schema": 1, "packet_id": packet.PacketID,
		"packet_hash": packetHash,
		"check":       packet.Check, "submitted_by": "synthetic-reviewer",
		"started_at": packet.IssuedAt, "finished_at": time.Now().UTC(),
		"before_input_digest": packet.InputDigest, "after_input_digest": packet.InputDigest,
		"result": result, "exit_code": exitCode,
		"tool_versions": []map[string]string{{"name": "synthetic-tool", "version": "1"}},
		"artifacts":     []map[string]string{{"name": "execution.log", "path": "execution.log", "sha256": fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))}},
	}
	data, err := json.Marshal(submission)
	if err != nil {
		t.Fatal(err)
	}
	path := "submitted/" + packet.PacketID + ".json"
	write(t, root, path, string(data))
	return path
}

func TestRelayWorkflow(t *testing.T) {
	t.Run("pass gates and replay", func(t *testing.T) {
		root := newProject(t, false)
		packet := relayPacket(t, root)
		if got := run(t, root, "", "gate", "pre-push"); got.code != 1 {
			t.Fatalf("pending allowed: %+v", got)
		}
		path := relaySubmission(t, root, packet, true)
		got := run(t, root, "", "relay", "submit", packet.PacketID, "--file", path, "--json")
		if got.code != 0 || !strings.Contains(got.stdout, "human-relayed") || strings.Contains(got.stdout, "synthetic-private-artifact-output") {
			t.Fatalf("submit: %+v", got)
		}
		if got := run(t, root, "", "status", "--json"); got.code != 0 || !strings.Contains(got.stdout, "human-relayed") {
			t.Fatalf("status: %+v", got)
		}
		if got := run(t, root, "", "gate", "pre-push"); got.code != 0 {
			t.Fatalf("push: %+v", got)
		}
		if got := run(t, root, "", "gate", "stop"); got.code != 0 {
			t.Fatalf("stop: %+v", got)
		}
		if got := run(t, root, "", "relay", "submit", packet.PacketID, "--file", path); got.code != 1 {
			t.Fatalf("replay: %+v", got)
		}
		write(t, root, "src/value.txt", "changed\n")
		if got := run(t, root, "", "gate", "pre-push"); got.code != 1 {
			t.Fatalf("changed allowed: %+v", got)
		}
	})
	t.Run("other required checks remain blocking", func(t *testing.T) {
		root := newProject(t, false)
		write(t, root, ".naru/config.yaml", strings.Replace(projectConfig, "code: [has-ok]", "code: [has-ok, has-readme]", 1))
		packet := relayPacket(t, root)
		path := relaySubmission(t, root, packet, true)
		if got := run(t, root, "", "relay", "submit", packet.PacketID, "--file", path); got.code != 0 {
			t.Fatal(got)
		}
		if got := run(t, root, "", "gate", "pre-push"); got.code != 1 || !strings.Contains(got.stderr, "has-readme") {
			t.Fatalf("other check bypass: %+v", got)
		}
		if got := run(t, root, "", "gate", "stop"); got.code != 2 {
			t.Fatalf("other check stop bypass: %+v", got)
		}
	})
	t.Run("failed submission", func(t *testing.T) {
		root := newProject(t, false)
		packet := relayPacket(t, root)
		path := relaySubmission(t, root, packet, false)
		if got := run(t, root, "", "relay", "submit", packet.PacketID, "--file", path); got.code != 1 || !strings.Contains(got.stdout, "fail") {
			t.Fatalf("failure: %+v", got)
		}
		if got := run(t, root, "", "gate", "pre-push"); got.code != 1 {
			t.Fatalf("failure allowed: %+v", got)
		}
	})
	t.Run("unverified survives submission", func(t *testing.T) {
		root := newProject(t, false)
		write(t, root, ".naru/state/unverified.json", `{"reason":"synthetic-test"}`)
		packet := relayPacket(t, root)
		path := relaySubmission(t, root, packet, true)
		if got := run(t, root, "", "relay", "submit", packet.PacketID, "--file", path); got.code != 0 {
			t.Fatal(got)
		}
		if got := run(t, root, "", "gate", "pre-push"); got.code != 1 || !strings.Contains(got.stderr, "NOT VERIFIED") {
			t.Fatalf("marker bypass: %+v", got)
		}
		if got := run(t, root, "", "status", "--json"); !strings.Contains(got.stdout, `"present": true`) && !strings.Contains(got.stdout, `"present":true`) {
			t.Fatalf("marker removed: %+v", got)
		}
		if got := run(t, root, "", "gate", "stop"); got.code != 0 {
			t.Fatalf("verified recovery: %+v", got)
		}
		if got := run(t, root, "", "gate", "pre-push"); got.code != 0 {
			t.Fatalf("recovered push: %+v", got)
		}
	})
	t.Run("CI executes actual command", func(t *testing.T) {
		root := newProject(t, false) // source contains no ok: actual command fails
		packet := relayPacket(t, root)
		path := relaySubmission(t, root, packet, true)
		if got := run(t, root, "", "relay", "submit", packet.PacketID, "--file", path); got.code != 0 {
			t.Fatal(got)
		}
		if got := run(t, root, "", "check", "--evidence", "ci"); got.code != 1 {
			t.Fatalf("CI reused relay: %+v", got)
		}
		if got := run(t, root, "", "status", "--json"); got.code != 1 || strings.Contains(got.stdout, "human-relayed") || !strings.Contains(got.stdout, `"ci"`) {
			t.Fatalf("CI authority: %+v", got)
		}
	})
	t.Run("strict usage and cancellation", func(t *testing.T) {
		root := newProject(t, false)
		for _, args := range [][]string{{"relay"}, {"relay", "new", "has-ok", "extra"}, {"relay", "submit", "id"}, {"relay", "new", "has-ok", "--unknown"}, {"relay", "new", "has-ok", "--target", "test", "--artifact", "execution.log", "--expires-in", "-1h"}, {"relay", "new", "has-ok", "--target", "test", "--artifact", "execution.log", "--expires-in", "0"}} {
			if got := run(t, root, "", args...); got.code != 64 {
				t.Fatalf("usage %v: %+v", args, got)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var out, errOut bytes.Buffer
		code := Run([]string{"relay", "new", "has-ok", "--target", "test", "--artifact", "execution.log"}, Env{Context: ctx, Dir: root, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
		if code != 130 {
			t.Fatalf("cancel exit=%d stderr=%s", code, errOut.String())
		}
		packet := relayPacket(t, root)
		path := relaySubmission(t, root, packet, true)
		out.Reset()
		errOut.Reset()
		code = Run([]string{"relay", "submit", packet.PacketID, "--file", path}, Env{Context: ctx, Dir: root, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
		if code != 130 {
			t.Fatalf("cancel submit exit=%d stderr=%s", code, errOut.String())
		}
		write(t, root, "malformed.json", `{"schema":1}`)
		if got := run(t, root, "", "relay", "submit", packet.PacketID, "--file", filepath.Join(root, "malformed.json")); got.code != 64 {
			t.Fatalf("malformed: %+v", got)
		}
	})
}
