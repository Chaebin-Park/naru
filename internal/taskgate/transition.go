package taskgate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Chaebin-Park/naru/internal/runlock"
)

// Execute owns the transition lock and always returns a structured verdict.
func (g *Gate) Execute(action, id, planPath string) (r Result, code int) {
	r = Result{Schema: Version, Action: action, Task: id, Reasons: []string{}, NextAction: ""}
	fail := func(err error) (Result, int) {
		r.Allowed = false
		r.Reasons = []string{"INSPECTION_ERROR"}
		r.NextAction = err.Error()
		code = 70
		var f *failure
		if errors.As(err, &f) {
			r.Reasons = []string{f.reason}
			code = f.code
		}
		if errors.Is(err, context.Canceled) || errors.Is(g.ctx.Err(), context.Canceled) {
			code = 130
			r.Reasons = []string{"INTERRUPTED"}
		}
		return r, code
	}
	if !taskID.MatchString(id) {
		return fail(&failure{"INVALID_ARGUMENT", 64, "expected NARU-nnn task ID"})
	}
	var err error
	r.Head, err = g.head()
	if err != nil {
		return fail(err)
	}
	// A separate nested lock root prevents collision with product check names.
	if err = g.prepareLocal(); err != nil {
		return fail(err)
	}
	lock, err := runlock.Acquire(g.local(), "transition")
	if errors.Is(err, runlock.ErrBusy) {
		return fail(deny("TRANSITION_BUSY", "another gate operation is active"))
	}
	if err != nil {
		return fail(err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			r, code = fail(err)
		}
	}()
	if w, e := g.workflow(); e == nil {
		r.Previous = w.Active
		if r.Previous == "" {
			r.Previous = w.Completed
		}
	}
	switch action {
	case "check":
		err = g.Check(id)
	case "resume":
		err = g.Resume(id)
	case "begin", "bootstrap":
		var p Plan
		var b []byte
		b, err = os.ReadFile(planPath) // #nosec G304 -- Explicit user-selected public plan input; no shell execution.
		if err == nil {
			err = decode(b, &p)
		}
		if err == nil {
			err = p.validate()
		}
		if err == nil {
			err = g.begin(id, p, action == "bootstrap")
		}
	case "verify":
		err = g.verify(id)
	case "close":
		err = g.closeTask(id)
	default:
		err = &failure{"INVALID_ARGUMENT", 64, "use check, begin, resume, verify, close or bootstrap"}
	}
	if err != nil {
		return fail(err)
	}
	final, err := g.head()
	if err != nil {
		return fail(err)
	}
	if (action == "check" || action == "resume" || action == "verify") && final != r.Head {
		return fail(deny("HEAD_CHANGED", "retry against the current HEAD"))
	}
	r.Head = final
	r.Allowed = true
	return r, 0
}
func (g *Gate) prepareLocal() error {
	for _, rel := range []string{".naru", ".naru/state", ".naru/state/taskgate"} {
		p := filepath.Join(g.Root, rel)
		info, err := os.Lstat(p)
		if err == nil && !info.IsDir() {
			return invalid("state ancestor is not a plain directory")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(p, 0o700); err != nil {
				return err
			}
		}
	}
	return nil
}
func (g *Gate) begin(id string, p Plan, bootstrap bool) error {
	if err := g.clean(); err != nil {
		return err
	}
	if err := g.settled(); err != nil {
		return err
	}
	head, err := g.head()
	if err != nil {
		return err
	}
	branch, err := g.branch()
	if err != nil {
		return err
	}
	var w Workflow
	if bootstrap {
		history, e := g.git("log", "--all", "--format=%H", "--", ledgerPath)
		if e != nil {
			return e
		}
		if strings.TrimSpace(history) != "" {
			return deny("WORKFLOW_ALREADY_INITIALIZED", "restore the existing ledger; bootstrap is one-time")
		}
		w = Workflow{Schema: Version, Branch: branch, ImportedBase: head}
	} else {
		if err = g.Check(id); err != nil {
			return err
		}
		w, err = g.workflow()
		if err != nil {
			return err
		}
	}
	t := Task{Version, id, w.Completed, head, "active", p, "", ""}
	w.Active = id
	if err = g.metadataCommit(head, w, t, "begin"); err != nil {
		return err
	}
	return g.Resume(id)
}
func (g *Gate) closeTask(id string) error {
	if err := g.clean(); err != nil {
		return err
	}
	if err := g.Resume(id); err != nil {
		return err
	}
	w, head, err := g.state()
	if err != nil {
		return err
	}
	t, err := g.task(head, id)
	if err != nil {
		return err
	}
	if err = g.implementation(t, head); err != nil {
		return err
	}
	evidence, err := g.loadEvidence(t, head)
	if err != nil {
		return err
	}
	t.Status = "completed"
	t.Implementation = head
	t.Evidence = digest(evidence)
	w.Active = ""
	w.Completed = id
	return g.metadataCommit(head, w, t, "complete")
}
func (g *Gate) metadataCommit(head string, w Workflow, t Task, action string) error {
	if err := g.clean(); err != nil {
		return err
	}
	current, err := g.head()
	if err != nil {
		return err
	}
	if current != head {
		return deny("HEAD_CHANGED", "retry transition")
	}
	if err = writeJSON(filepath.Join(g.Root, taskPath(t.ID)), t); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(g.Root, ledgerPath), w); err != nil {
		return err
	}
	// Stage only the two generated paths. Refuse concurrent staged work before commit.
	staged, err := g.git("diff", "--cached", "--name-only", "-z")
	if err != nil {
		return err
	}
	if staged != "" {
		return deny("INDEX_DIRTY", "concurrent staging; recover pending metadata")
	}
	if _, err = g.git("add", "--", ledgerPath, taskPath(t.ID)); err != nil {
		return err
	}
	paths, err := g.git("diff", "--cached", "--name-only", "-z")
	if err != nil {
		return err
	}
	list := strings.Split(strings.TrimSuffix(paths, "\x00"), "\x00")
	if len(list) != 2 {
		return deny("INDEX_DIRTY", "unexpected staged paths")
	}
	for _, p := range list {
		if p != ledgerPath && p != taskPath(t.ID) {
			return deny("INDEX_DIRTY", "unexpected staged path")
		}
	}
	current, err = g.head()
	if err != nil {
		return err
	}
	if current != head {
		return deny("HEAD_CHANGED", "recover pending metadata")
	}
	// --only keeps unrelated concurrent index entries out of this metadata commit.
	_, err = g.git("commit", "--only", "-m", "chore(tasks): "+action+" "+t.ID, "-m", "Task: "+t.ID+"\nTask-Action: "+action, "--", ledgerPath, taskPath(t.ID))
	if err != nil {
		return err
	}
	final, err := g.head()
	if err != nil {
		return err
	}
	parent, err := g.git("show", "-s", "--format=%P", final)
	if err != nil {
		return err
	}
	if strings.TrimSpace(parent) != head {
		return deny("HEAD_CHANGED", "transition raced with another commit")
	}
	if err = g.metadataOnly(final, t.ID); err != nil {
		return err
	}
	if err = g.trailers(final, t.ID, action); err != nil {
		return err
	}
	stored, err := g.task(final, t.ID)
	if err != nil {
		return err
	}
	actual, err := g.workflow()
	if err != nil {
		return err
	}
	if digest(stored) != digest(t) || digest(actual) != digest(w) {
		return invalid("committed metadata differs from requested transition")
	}
	branch, err := g.branch()
	if err != nil {
		return err
	}
	if branch != w.Branch {
		return deny("BRANCH_MISMATCH", "branch changed during transition")
	}
	return g.clean()
}
