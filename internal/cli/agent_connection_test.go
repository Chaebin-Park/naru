package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func agentStopPayload(t *testing.T, root, agent string, active bool) string {
	t.Helper()
	fields := map[string]any{
		"hook_event_name":        "Stop",
		"cwd":                    root,
		"session_id":             "synthetic-session",
		"transcript_path":        "synthetic-transcript.jsonl",
		"stop_hook_active":       active,
		"last_assistant_message": "Task complete",
	}
	if agent == "codex" {
		fields["turn_id"] = "synthetic-turn"
		fields["model"] = "synthetic-model"
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// These tests replay documented payloads; they do not claim live agent coverage.
func TestAgentHookPayloads(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			root := newProject(t, false)
			stop := func(active bool) result {
				return run(t, root, agentStopPayload(t, root, agent, active), "gate", "stop")
			}
			assertBlock := func(got result, reason string) {
				t.Helper()
				if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, reason) {
					t.Fatalf("block contract: %+v", got)
				}
			}
			assertBlock(stop(false), "never run")
			write(t, root, "src/value.txt", "ok\n")
			if got := run(t, root, "", "check"); got.code != 0 {
				t.Fatal(got)
			}
			if got := stop(false); got.code != 0 || got.stdout != "" {
				t.Fatalf("verified completion contract: %+v", got)
			}
			write(t, root, "src/value.txt", "ok, changed\n")
			assertBlock(stop(false), "inputs or check definition changed")
			assertBlock(stop(true), "inputs or check definition changed")
			assertBlock(stop(true), "inputs or check definition changed")
			got := stop(true)
			var warning struct {
				SystemMessage string `json:"systemMessage"`
			}
			if got.code != 0 || json.Unmarshal([]byte(got.stdout), &warning) != nil || !strings.Contains(warning.SystemMessage, "NOT VERIFIED") {
				t.Fatalf("unverified JSON warning: %+v", got)
			}
			if got := run(t, root, "", "status"); !strings.Contains(got.stdout, "WITHOUT verification") {
				t.Fatalf("missing persistent warning: %+v", got)
			}
			if got := run(t, root, "", "check"); got.code != 0 {
				t.Fatal(got)
			}
			if got := stop(false); got.code != 0 || got.stdout != "" {
				t.Fatalf("verified recovery: %+v", got)
			}
			if got := run(t, root, "", "status"); got.code != 0 || strings.Contains(got.stdout, "WITHOUT verification") {
				t.Fatalf("warning did not clear: %+v", got)
			}
		})
	}
}

func TestCodexPatchGuard(t *testing.T) {
	root := newProject(t, false)
	sub := filepath.Join(root, "src")
	if err := os.Symlink(filepath.Join(root, ".naru"), filepath.Join(root, "evidence-link")); err != nil {
		t.Fatal(err)
	}
	wrap := func(body string) string { return "*** Begin Patch\n" + body + "\n*** End Patch\n" }
	cases := []struct {
		name, cwd, patch string
		block            bool
	}{
		{"add protected", root, wrap("*** Add File: .naru/fake.json\n+{}"), true},
		{"update protected", root, wrap("*** Update File: .naru/config.yaml\n@@\n-old\n+new"), true},
		{"delete protected", root, wrap("*** Delete File: .naru/config.yaml"), true},
		{"move into protected", root, wrap("*** Update File: src/value.txt\n*** Move to: .naru/fake.json\n@@\n-old\n+new"), true},
		{"move out of protected", root, wrap("*** Update File: .naru/config.yaml\n*** Move to: stolen.yaml\n@@\n-old\n+new"), true},
		{"multiple files", root, wrap("*** Add File: notes.txt\n+note\n*** Delete File: .naru/config.yaml"), true},
		{"absolute protected", root, wrap("*** Add File: " + filepath.Join(root, ".naru/fake.json") + "\n+{}"), true},
		{"relative from subdir", sub, wrap("*** Add File: ../.naru/fake.json\n+{}"), true},
		{"symlink protected", root, wrap("*** Add File: evidence-link/fake.json\n+{}"), true},
		{"normal source", root, wrap("*** Update File: src/value.txt\n@@\n-bad\n+ok"), false},
		{"normal add", root, wrap("*** Add File: notes.txt\n+note"), false},
		{"normal delete", root, wrap("*** Delete File: src/value.txt"), false},
		{"normal move", root, wrap("*** Update File: src/value.txt\n*** Move to: src/new.txt\n@@\n-bad\n+ok"), false},
		{"similar name", root, wrap("*** Add File: docs/.naru-notes.md\n+note"), false},
		{"patch text in source", root, wrap("*** Add File: notes.txt\n+*** Add File: .naru/fake.json"), false},
		{"empty command", root, "", true},
		{"missing end", root, "*** Begin Patch\n*** Add File: notes.txt\n+note", true},
		{"missing begin", root, "*** Add File: notes.txt\n+note\n*** End Patch", true},
		{"missing path", root, wrap("*** Delete File: "), true},
		{"unknown operation", root, wrap("*** Rename File: .naru/config.yaml"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, tc.cwd, toolPayload(tc.cwd, "apply_patch", map[string]string{"command": tc.patch}), "gate", "pre-tool")
			wantCode := 0
			if tc.block {
				wantCode = 2
			}
			if got.code != wantCode {
				t.Fatalf("patch exit=%d want %d: %+v", got.code, wantCode, got)
			}
			if tc.block && (got.stderr == "" || got.stdout != "") {
				t.Fatalf("block output: %+v", got)
			}
		})
	}
	for _, command := range []string{"cat .naru/config.yaml", "naru check", "naru status"} {
		got := run(t, root, toolPayload(root, "Bash", map[string]string{"command": command}), "gate", "pre-tool")
		if got.code != 0 {
			t.Fatalf("allowed command %q: %+v", command, got)
		}
	}
	for _, command := range []string{"rm .naru/config.yaml", "naru mode docs"} {
		got := run(t, root, toolPayload(root, "Bash", map[string]string{"command": command}), "gate", "pre-tool")
		if got.code != 2 {
			t.Fatalf("protected command %q: %+v", command, got)
		}
	}
	for _, input := range []any{map[string]any{}, map[string]any{"command": 123}, "not-an-object"} {
		data, err := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "apply_patch", "tool_input": input})
		if err != nil {
			t.Fatal(err)
		}
		if got := run(t, root, string(data), "gate", "pre-tool"); got.code != 2 {
			t.Fatalf("malformed supported patch accepted: %+v", got)
		}
	}
	for _, tc := range []struct {
		name     string
		cwd      string
		disabled bool
		wantCode int
	}{
		{"malformed inside project", root, false, 2},
		{"malformed outside project", t.TempDir(), false, 0},
		{"malformed with gate disabled", root, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_PROJECT_DIR", "")
			t.Setenv("NARU_GATE", "")
			t.Setenv("NARU_MODE", "")
			if tc.disabled {
				t.Setenv("NARU_GATE", "off")
			}
			data, err := json.Marshal(map[string]any{
				"hook_event_name": "PreToolUse",
				"cwd":             tc.cwd,
				"tool_name":       "apply_patch",
				"tool_input":      map[string]any{"command": 123},
			})
			if err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := Run([]string{"gate", "pre-tool"}, Env{
				Stdin: strings.NewReader(string(data)), Stdout: &out, Stderr: &errOut, Dir: tc.cwd,
			})
			if code != tc.wantCode {
				t.Fatalf("malformed patch exit=%d want %d: stdout=%q stderr=%q", code, tc.wantCode, out.String(), errOut.String())
			}
		})
	}
}
