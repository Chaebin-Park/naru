package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/status"
)

// Invoke without run's environment reset so precedence is tested at the CLI boundary.
func contractRun(root string, args ...string) result {
	var out, errb bytes.Buffer
	code := Run(args, Env{Dir: root, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb})
	return result{code, out.String(), errb.String()}
}

func TestModePrecedenceAtCLI(t *testing.T) {
	for _, tc := range []struct{ name, stored, env, flag, want string }{
		{"default", "", "", "", "code"},
		{"stored", "docs", "", "", "docs"},
		{"environment", "docs", "code", "", "code"},
		{"flag", "code", "code", "docs", "docs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newProject(t, false)
			t.Setenv("NARU_MODE", tc.env)
			if tc.stored != "" {
				write(t, root, ".naru/state/mode", tc.stored+"\n")
			}
			args := []string{"status", "--json"}
			if tc.flag != "" {
				args = append(args, "--mode", tc.flag)
			}
			r := contractRun(root, args...)
			var rep status.Report
			if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
				t.Fatal(err)
			}
			if r.code != 1 || rep.Mode != tc.want || rep.Satisfied || len(rep.Checks) != 1 {
				t.Fatalf("unexpected report: %+v, %+v", r, rep)
			}
		})
	}
}

func TestEvidencePrecedenceAtCLI(t *testing.T) {
	for _, tc := range []struct{ name, env, ci, flag, want string }{
		{"default", "", "", "", "local"},
		{"ci", "", "true", "", "ci"},
		{"environment", "agent-run", "true", "", "agent-run"},
		{"flag", "agent-run", "true", "local", "local"},
		{"invalid", "unknown", "true", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newProject(t, false)
			write(t, root, "src/value.txt", "ok\n")
			t.Setenv("NARU_EVIDENCE", tc.env)
			t.Setenv("CI", tc.ci)
			args := []string{"check"}
			if tc.flag != "" {
				args = append(args, "--evidence", tc.flag)
			}
			r := run(t, root, "", args...)
			rec, err := receipt.NewStateStore(root).Current("has-ok")
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if r.code != 64 || rec != nil {
					t.Fatalf("invalid evidence must not run or write a receipt: %+v, %+v", r, rec)
				}
			} else if r.code != 0 || rec == nil || rec.Provenance.Kind != tc.want {
				t.Fatalf("unexpected evidence: %+v, %+v", r, rec)
			}
		})
	}
}

func TestCorruptReceiptBlocksEvaluation(t *testing.T) {
	root := newProject(t, false)
	write(t, root, "src/value.txt", "ok\n")
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	write(t, root, ".naru/state/checks/has-ok.json", `{"schema":2,`)
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"status", "--json"}, 1},
		{[]string{"gate", "stop"}, 2},
		{[]string{"gate", "pre-push"}, 1},
	} {
		r := run(t, root, "", tc.args...)
		if r.code != tc.code || !strings.Contains(r.stderr+r.stdout, "corrupt") {
			t.Fatalf("%v: %+v", tc.args, r)
		}
	}
	if r := run(t, root, "", "check"); r.code != 70 {
		t.Fatalf("silently overwrote corrupt authority: %+v", r)
	}
	// Explicit manual recovery preserves the corrupt original before rerunning.
	if err := os.Rename(filepath.Join(root, ".naru/state/checks/has-ok.json"), filepath.Join(root, "corrupt-state.backup")); err != nil {
		t.Fatal(err)
	}
	if r := run(t, root, "", "check"); r.code != 0 {
		t.Fatal(r)
	}
	if r := run(t, root, "", "status"); r.code != 0 {
		t.Fatalf("rerun did not restore valid evidence: %+v", r)
	}
}

func TestReceiptV1Fixture(t *testing.T) {
	data, err := os.ReadFile("testdata/receipt-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	root := newProject(t, false)
	write(t, root, ".naru/receipts/has-ok.json", string(data))
	store := receipt.NewStore(root)
	rec, err := store.Get("has-ok")
	if err != nil || rec == nil {
		t.Fatalf("read fixture: %v, %+v", err, rec)
	}
	if rec.Schema != 1 || rec.Check != "has-ok" || rec.Result != "pass" || rec.Evidence != "local" || rec.FileCount != 1 || rec.DurationMS != 12 {
		t.Fatalf("fixture fields lost: %+v", rec)
	}
	// A structurally valid receipt for other inputs must never satisfy this project.
	if r := run(t, root, "", "status"); r.code != 1 || !strings.Contains(r.stdout, "stale") {
		t.Fatalf("fixture incorrectly accepted: %+v", r)
	}
	if err := store.Put(*rec); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(root, ".naru/receipts/has-ok.json")) // #nosec G304 -- Reads a synthetic fixture or a file in the test-owned temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(actual, &got); err != nil {
		t.Fatal(err)
	}
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	if !bytes.Equal(w, g) {
		t.Fatalf("schema 1 round trip changed fields:\n%s", actual)
	}
}
