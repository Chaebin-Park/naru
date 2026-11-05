package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestAgentConnectionTemplates checks the shipped fragments and invokes their
// actual command arguments. It does not claim agent loading or live enforcement.
func TestAgentConnectionTemplates(t *testing.T) {
	for _, tc := range []struct {
		name, path, matcher string
		allow               []string
	}{
		{"claude", "../../examples/agent-hooks/claude.settings.json", "Write|Edit|NotebookEdit|Bash", []string{"Bash(naru check:*)", "Bash(naru status:*)"}},
		{"codex", "../../examples/agent-hooks/codex.hooks.json", "Bash|apply_patch", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.path) // #nosec G304 -- Fixed repository example paths from the test table; no external input.
			if err != nil {
				t.Fatal(err)
			}
			// Deliberately exclude async, shell wrappers, and alternate event fields
			// from the fragment contract so a change cannot silently disable blocking.
			var template struct {
				Permissions *struct {
					Allow []string `json:"allow"`
				} `json:"permissions,omitempty"`
				Hooks map[string][]struct {
					Matcher string `json:"matcher,omitempty"`
					Hooks   []struct {
						Type    string `json:"type"`
						Command string `json:"command"`
					} `json:"hooks"`
				} `json:"hooks"`
			}
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&template); err != nil {
				t.Fatal(err)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatalf("unexpected trailing JSON: %v", err)
			}
			if tc.allow == nil {
				if template.Permissions != nil {
					t.Fatal("Codex template must not use Claude permission syntax")
				}
			} else if template.Permissions == nil || !reflect.DeepEqual(template.Permissions.Allow, tc.allow) {
				t.Fatal("Claude template must allow only check and status")
			}
			if len(template.Hooks) != 2 {
				t.Fatalf("unexpected events: %+v", template.Hooks)
			}
			root := newProject(t, false)
			for _, event := range []string{"PreToolUse", "Stop"} {
				entries := template.Hooks[event]
				if len(entries) != 1 || len(entries[0].Hooks) != 1 {
					t.Fatalf("%s must have exactly one handler", event)
				}
				matcher, command, payload := tc.matcher, "naru gate pre-tool", toolPayload(root, "Bash", map[string]string{"command": "rm -rf .naru"})
				if event == "Stop" {
					matcher, command, payload = "", "naru gate stop", stopPayload(root)
				}
				hook := entries[0].Hooks[0]
				if entries[0].Matcher != matcher || hook.Type != "command" || hook.Command != command {
					t.Fatalf("unsupported %s configuration: %+v", event, entries[0])
				}
				args := strings.Fields(hook.Command)[1:]
				if got := run(t, root, payload, args...); got.code != 2 || got.stderr == "" {
					t.Fatalf("%s command must block with a reason: %+v", event, got)
				}
			}
		})
	}
}
