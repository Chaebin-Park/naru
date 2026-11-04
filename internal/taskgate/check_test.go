package taskgate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Gate {
	t.Helper()
	r := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "Task Test"}, {"config", "user.email", "task@example.invalid"}} {
		c := exec.Command("git", args...) // #nosec G204 -- Fixed fixture setup arguments in a test-owned temporary repository.
		c.Dir = r
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	writeTest(t, r, ".gitignore", "/.naru/state/\n")
	writeTest(t, r, "input.txt", "base")
	g, e := Open(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	gitTest(t, g, "add", ".gitignore", "input.txt")
	gitTest(t, g, "commit", "-qm", "fixture")
	return g
}
func writeTest(t *testing.T, root, path, text string) {
	t.Helper()
	p := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(p), 0o700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(text), 0o600); e != nil {
		t.Fatal(e)
	}
}
func gitTest(t *testing.T, g *Gate, args ...string) string {
	t.Helper()
	s, e := g.git(args...)
	if e != nil {
		t.Fatal(e)
	}
	return strings.TrimSpace(s)
}
func jsonTest(t *testing.T, g *Gate, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	writeTest(t, g.Root, path, string(b))
}
func planFixture() Plan {
	return Plan{"test task", Profile, []Criterion{{"AC-1", "required behavior", "example.test/app/TestRequired"}}, []string{}}
}
func startTest(t *testing.T, g *Gate) Task {
	t.Helper()
	base := gitTest(t, g, "rev-parse", "HEAD")
	task := Task{Version, "NARU-100", "", base, "active", planFixture(), "", ""}
	jsonTest(t, g, ledgerPath, Workflow{Version, "main", task.ID, "", base})
	jsonTest(t, g, taskPath(task.ID), task)
	gitTest(t, g, "add", "docs/tasks")
	gitTest(t, g, "commit", "-qm", "start\n\nTask: NARU-100\nTask-Action: begin")
	return task
}
func implementationTest(t *testing.T, g *Gate) string {
	t.Helper()
	writeTest(t, g.Root, "input.txt", "implemented")
	gitTest(t, g, "add", "input.txt")
	gitTest(t, g, "commit", "-qm", "implement\n\nTask: NARU-100")
	return gitTest(t, g, "rev-parse", "HEAD")
}
func reasonTest(t *testing.T, e error, want string) {
	t.Helper()
	var f *failure
	if !errors.As(e, &f) || f.reason != want {
		t.Fatalf("want %s, got %v", want, e)
	}
}
func TestStrictTaskContract(t *testing.T) {
	p := planFixture()
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, data := range []string{strings.Replace(string(b), `"title":"test task",`, "", 1), strings.Replace(string(b), `"title":"test task"`, `"title":"x","title":"y"`, 1), strings.Replace(string(b), `"limitations":[]`, `"limitations":null`, 1), string(b) + `{}`, strings.Replace(string(b), `"title":`, `"unknown":0,"title":`, 1)} {
		var got Plan
		if decode([]byte(data), &got) == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	var got Plan
	if e = decode(b, &got); e != nil {
		t.Fatal(e)
	}
	if e = got.validate(); e != nil {
		t.Fatal(e)
	}
}
func TestIncompleteTaskBlocksCleanAndDirtyTree(t *testing.T) {
	g := fixture(t)
	reasonTest(t, g.Check("NARU-101"), "WORKFLOW_UNINITIALIZED")
	startTest(t, g)
	reasonTest(t, g.Check("NARU-101"), "PREVIOUS_TASK_INCOMPLETE")
	writeTest(t, g.Root, "untracked file.txt", "unfinished")
	reasonTest(t, g.Check("NARU-101"), "PREVIOUS_TASK_INCOMPLETE")
	if e := g.Resume("NARU-100"); e != nil {
		t.Fatal(e)
	}
	reasonTest(t, g.Resume("NARU-101"), "ACTIVE_TASK_MISMATCH")
	reasonTest(t, g.clean(), "WORKTREE_DIRTY")
	gitTest(t, g, "add", "untracked file.txt")
	reasonTest(t, g.clean(), "WORKTREE_DIRTY")
}
func TestImplementationMustMatchTaskAndFrozenPlan(t *testing.T) {
	g := fixture(t)
	task := startTest(t, g)
	h := gitTest(t, g, "rev-parse", "HEAD")
	reasonTest(t, g.implementation(task, h), "COMMIT_MISSING")
	tip := implementationTest(t, g)
	if e := g.implementation(task, tip); e != nil {
		t.Fatal(e)
	}
	task.Plan.Title = "weakened"
	reasonTest(t, g.implementation(task, tip), "PLAN_CHANGED")
	writeTest(t, g.Root, "input.txt", "other")
	gitTest(t, g, "add", "input.txt")
	gitTest(t, g, "commit", "-qm", "other\n\nTask: NARU-101")
	task.Plan = planFixture()
	reasonTest(t, g.implementation(task, gitTest(t, g, "rev-parse", "HEAD")), "TASK_TRAILER_MISMATCH")
}
func TestClosureCannotContainProductChanges(t *testing.T) {
	g := fixture(t)
	task := startTest(t, g)
	tip := implementationTest(t, g)
	task.Status = "completed"
	task.Implementation = tip
	task.Evidence = strings.Repeat("a", 64)
	w := Workflow{Version, "main", "", task.ID, task.Base}
	jsonTest(t, g, ledgerPath, w)
	jsonTest(t, g, taskPath(task.ID), task)
	writeTest(t, g.Root, "input.txt", "hidden edit")
	gitTest(t, g, "add", "docs/tasks", "input.txt")
	gitTest(t, g, "commit", "-qm", "complete\n\nTask: NARU-100\nTask-Action: complete")
	reasonTest(t, g.Check("NARU-101"), "CLOSURE_INVALID")
}
func TestBranchAndPendingMetadataAreRejected(t *testing.T) {
	g := fixture(t)
	startTest(t, g)
	writeTest(t, g.Root, ledgerPath, "{}")
	reasonTest(t, g.Resume("NARU-100"), "PLAN_CHANGED")
	gitTest(t, g, "switch", "-qc", "other")
	reasonTest(t, g.Resume("NARU-100"), "BRANCH_MISMATCH")
}
