package taskgate

import "strings"

func (g *Gate) state() (Workflow, string, error) {
	w, e := g.workflow()
	if e != nil {
		return w, "", e
	}
	h, e := g.head()
	if e != nil {
		return w, h, e
	}
	b, e := g.branch()
	if e != nil {
		return w, h, e
	}
	if b != w.Branch {
		return w, h, deny("BRANCH_MISMATCH", b)
	}
	return w, h, g.settled()
}
func (g *Gate) Resume(id string) error {
	w, head, e := g.state()
	if e != nil {
		return e
	}
	if w.Active != id {
		return deny("ACTIVE_TASK_MISMATCH", "resume only "+w.Active)
	}
	t, e := g.task("HEAD", id)
	if e != nil {
		return e
	}
	if t.Status != "active" || t.Previous != w.Completed {
		return invalid("active ledger points to completed task")
	}
	// Ordinary product modifications are allowed, but pending ledger changes are not.
	s, e := g.git("status", "--porcelain=v1", "-z", "--", "docs/tasks")
	if e != nil {
		return e
	}
	if s != "" {
		return deny("PLAN_CHANGED", "commit or recover task metadata before resuming")
	}
	return g.history(t, head, false)
}
func (g *Gate) Check(next string) error {
	w, head, e := g.state()
	if e != nil {
		return e
	}
	if w.Active != "" {
		return deny("PREVIOUS_TASK_INCOMPLETE", "resume "+w.Active+", verify and close it first")
	}
	if next == w.Completed {
		return deny("TASK_ALREADY_EXISTS", next)
	}
	if _, e = g.git("cat-file", "-e", "HEAD:"+taskPath(next)); e == nil {
		return deny("TASK_ALREADY_EXISTS", next)
	}
	if e = g.clean(); e != nil {
		return e
	}
	t, e := g.task(head, w.Completed)
	if e != nil {
		return e
	}
	if t.Status != "completed" {
		return deny("PREVIOUS_TASK_INCOMPLETE", t.ID)
	}
	if e = g.trailers(head, t.ID, "complete"); e != nil {
		return e
	}
	if e = g.metadataOnly(head, t.ID); e != nil {
		return e
	}
	parent, e := g.git("show", "-s", "--format=%P", head)
	if e != nil {
		return e
	}
	if strings.TrimSpace(parent) != t.Implementation {
		return deny("CLOSURE_INVALID", "completion parent differs from verified implementation")
	}
	old, e := g.task(t.Implementation, t.ID)
	if e != nil {
		return e
	}
	want := t
	want.Status = "active"
	want.Implementation = ""
	want.Evidence = ""
	if digest(old) != digest(want) {
		return deny("CLOSURE_INVALID", "completion changed the task plan")
	}
	if e = g.implementation(t, t.Implementation); e != nil {
		return e
	}
	if e = g.evidence(t, t.Implementation); e != nil {
		return e
	}
	return g.clean()
}
