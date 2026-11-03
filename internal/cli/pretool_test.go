package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func toolPayload(cwd, tool string, input map[string]string) string {
	b, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": cwd, "tool_name": tool, "tool_input": input})
	return string(b)
}

func TestPreToolGuardsNaruDirectory(t *testing.T) {
	root := newProject(t, false)
	sub := filepath.Join(root, "src")
	cases := []struct {
		name  string
		cwd   string
		tool  string
		input map[string]string
		block bool
	}{
		{"edit config", root, "Edit", map[string]string{"file_path": filepath.Join(root, ".naru/config.yaml")}, true},
		{"write receipt, relative from subdir", sub, "Write", map[string]string{"file_path": "../.naru/receipts/has-ok.json"}, true},
		{"notebook in .naru", root, "NotebookEdit", map[string]string{"notebook_path": ".naru/x.ipynb"}, true},
		{"edit source", root, "Edit", map[string]string{"file_path": filepath.Join(root, "src/value.txt")}, false},
		{"file named like .naru elsewhere", root, "Write", map[string]string{"file_path": filepath.Join(root, "docs/.naru-notes.md")}, false},
		{"bash redirect into receipts", root, "Bash", map[string]string{"command": `echo '{}' > .naru/receipts/has-ok.json`}, true},
		{"bash rm receipts", root, "Bash", map[string]string{"command": "rm -rf .naru/receipts"}, true},
		{"bash sed -i config", root, "Bash", map[string]string{"command": "sed -i '' 's/grep/true/' .naru/config.yaml"}, true},
		{"bash python rewrite", root, "Bash", map[string]string{"command": `python3 -c "open('.naru/config.yaml','w')"`}, true},
		{"bash mode change", root, "Bash", map[string]string{"command": "naru mode docs"}, true},
		{"bash mode clear chained", root, "Bash", map[string]string{"command": "cd src && naru mode --clear"}, true},
		{"bash read config", root, "Bash", map[string]string{"command": "cat .naru/config.yaml"}, false},
		// Seen live: a read-only inspection with 2>/dev/null was wrongly blocked.
		{"bash read logs with stderr redirect", root, "Bash", map[string]string{"command": "cat .naru/logs/x.log; ls .naru; cat .naru/*.y* .naru/*.json 2>/dev/null | head -50"}, false},
		{"bash grep with 2>&1", root, "Bash", map[string]string{"command": "grep -r answer .naru/ 2>&1 | head"}, false},
		{"bash copy log out", root, "Bash", map[string]string{"command": "cp .naru/logs/x.log /tmp/x.log"}, false},
		{"bash copy into naru", root, "Bash", map[string]string{"command": "cp fake.json .naru/receipts/has-ok.json"}, true},
		{"bash append redirect", root, "Bash", map[string]string{"command": "echo x >> .naru/config.yaml"}, true},
		{"bash tee", root, "Bash", map[string]string{"command": "echo x | tee .naru/state/mode"}, true},
		{"bash find delete", root, "Bash", map[string]string{"command": "find .naru -name '*.json' -delete"}, true},
		{"bash git restore", root, "Bash", map[string]string{"command": "git restore .naru/config.yaml"}, true},
		{"bash env prefix rm", root, "Bash", map[string]string{"command": "FOO=1 rm .naru/receipts/has-ok.json"}, true},
		{"bash naru check", root, "Bash", map[string]string{"command": "naru check has-ok"}, false},
		{"bash show mode", root, "Bash", map[string]string{"command": "naru mode"}, false},
		{"bash unrelated", root, "Bash", map[string]string{"command": "go test ./..."}, false},
		{"read tool", root, "Read", map[string]string{"file_path": filepath.Join(root, ".naru/config.yaml")}, false},
	}
	for _, c := range cases {
		r := run(t, c.cwd, toolPayload(c.cwd, c.tool, c.input), "gate", "pre-tool")
		if blocked := r.code == 2; blocked != c.block {
			t.Errorf("%s: blocked=%v want %v (%+v)", c.name, blocked, c.block, r)
		}
		if c.block && !strings.Contains(r.stderr, "naru: blocked") {
			t.Errorf("%s: missing reason: %q", c.name, r.stderr)
		}
	}
}

func TestPreToolOutsideProjectAndDisabled(t *testing.T) {
	plain := t.TempDir()
	if r := run(t, plain, toolPayload(plain, "Write", map[string]string{"file_path": filepath.Join(plain, ".naru/config.yaml")}), "gate", "pre-tool"); r.code != 0 {
		t.Fatalf("outside a naru project must allow: %+v", r)
	}
	root := newProject(t, false)
	t.Setenv("NARU_GATE", "off")
	var out, errb strings.Builder
	code := Run([]string{"gate", "pre-tool"}, Env{Stdin: strings.NewReader(toolPayload(root, "Bash", map[string]string{"command": "rm -rf .naru"})), Stdout: &out, Stderr: &errb, Dir: root})
	if code != 0 {
		t.Fatalf("NARU_GATE=off must allow: %d %s", code, errb.String())
	}
	if r := run(t, root, "not json", "gate", "pre-tool"); r.code != 0 {
		t.Fatalf("garbage payload: %+v", r)
	}
}

// Giving up must reach the human (systemMessage) and stay visible in status.
func TestGiveUpIsVisibleToHumanAndInStatus(t *testing.T) {
	root := newProject(t, false)
	run(t, root, stopChain(root, false), "gate", "stop")
	run(t, root, stopChain(root, true), "gate", "stop")
	run(t, root, stopChain(root, true), "gate", "stop")
	r := run(t, root, stopChain(root, true), "gate", "stop")
	var msg struct {
		SystemMessage string `json:"systemMessage"`
	}
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &msg) != nil || !strings.Contains(msg.SystemMessage, "NOT VERIFIED") {
		t.Fatalf("give-up must print a systemMessage for the user: %+v", r)
	}
	if s := run(t, root, "", "status"); !strings.Contains(s.stdout, "WITHOUT verification") {
		t.Fatalf("status must keep showing the unverified stop: %+v", s)
	}
	write(t, root, "src/value.txt", "ok\n")
	run(t, root, "", "check")
	run(t, root, stopChain(root, false), "gate", "stop")
	if s := run(t, root, "", "status"); strings.Contains(s.stdout, "WITHOUT verification") {
		t.Fatalf("a verified stop must clear the warning: %+v", s)
	}
}
