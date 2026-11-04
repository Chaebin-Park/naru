package taskgate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Gate struct {
	Root string
	ctx  context.Context
}

func Open(ctx context.Context, root string) (*Gate, error) {
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		if os.Getenv(key) != "" {
			return nil, invalid("unset " + key)
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	g := &Gate{root, ctx}
	top, err := g.git("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	top, err = filepath.EvalSymlinks(strings.TrimSpace(top))
	if err != nil || top != root {
		return nil, invalid("run from Git working-tree root")
	}
	return g, nil
}
func (g *Gate) git(args ...string) (string, error) {
	cmd := exec.CommandContext(g.ctx, "git", args...) // #nosec G204 -- Internal argv use Git objects and validated task IDs; no shell interpolation.
	cmd.Dir = g.Root
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, b)
	}
	return string(b), nil
}
func (g *Gate) head() (string, error) {
	s, e := g.git("rev-parse", "HEAD")
	return strings.TrimSpace(s), e
}
func (g *Gate) branch() (string, error) {
	s, e := g.git("symbolic-ref", "--quiet", "--short", "HEAD")
	return strings.TrimSpace(s), e
}
func (g *Gate) clean() error {
	s, e := g.git("status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if e != nil {
		return e
	}
	if s != "" {
		return deny("WORKTREE_DIRTY", "commit current task changes before transition")
	}
	return nil
}
func (g *Gate) settled() error {
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"} {
		p, err := g.git("rev-parse", "--git-path", name)
		if err != nil {
			return err
		}
		p = strings.TrimSpace(p)
		if !filepath.IsAbs(p) {
			p = filepath.Join(g.Root, p)
		}
		if _, err = os.Lstat(p); err == nil {
			return deny("GIT_OPERATION_ACTIVE", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func (g *Gate) readAt(commit, path string, dst any) error {
	b, e := g.git("show", commit+":"+path)
	if e != nil {
		return e
	}
	return decode([]byte(b), dst)
}
func (g *Gate) workflow() (Workflow, error) {
	var w Workflow
	if _, e := g.git("cat-file", "-e", "HEAD:"+ledgerPath); e != nil {
		return w, deny("WORKFLOW_UNINITIALIZED", "restore the committed ledger or perform explicit first-time bootstrap")
	}
	if e := g.readAt("HEAD", ledgerPath, &w); e != nil {
		return w, e
	}
	return w, w.validate()
}
func (g *Gate) task(commit, id string) (Task, error) {
	var t Task
	if !taskID.MatchString(id) {
		return t, invalid("task ID")
	}
	if e := g.readAt(commit, taskPath(id), &t); e != nil {
		return t, e
	}
	if t.ID != id {
		return t, invalid("task file identity")
	}
	return t, t.validate()
}
func (g *Gate) paths(commit string) ([]string, error) {
	s, e := g.git("diff-tree", "--no-commit-id", "--name-only", "-r", "-z", commit)
	if e != nil {
		return nil, e
	}
	if s == "" {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(s, "\x00"), "\x00"), nil
}
func (g *Gate) trailers(commit, id, action string) error {
	s, e := g.git("show", "-s", "--format=%(trailers:key=Task,valueonly)", commit)
	if e != nil {
		return e
	}
	if strings.TrimSpace(s) != id {
		return deny("TASK_TRAILER_MISMATCH", commit)
	}
	s, e = g.git("show", "-s", "--format=%(trailers:key=Task-Action,valueonly)", commit)
	if e != nil {
		return e
	}
	if strings.TrimSpace(s) != action {
		return deny("TASK_TRAILER_MISMATCH", "unexpected action on "+commit)
	}
	return nil
}
func (g *Gate) metadataOnly(commit, id string) error {
	paths, e := g.paths(commit)
	if e != nil {
		return e
	}
	if len(paths) != 2 {
		return deny("CLOSURE_INVALID", "expected workflow and task metadata only")
	}
	for _, p := range paths {
		if p != ledgerPath && p != taskPath(id) {
			return deny("CLOSURE_INVALID", "unexpected metadata commit path")
		}
	}
	return nil
}

// implementation verifies a linear range, immutable plan, and task attribution.
func (g *Gate) implementation(t Task, tip string) error {
	if _, e := g.git("merge-base", "--is-ancestor", t.Base, tip); e != nil {
		return deny("COMMIT_MISSING", "task base is not an ancestor")
	}
	s, e := g.git("rev-list", "--reverse", t.Base+".."+tip)
	if e != nil {
		return e
	}
	commits := strings.Fields(s)
	if len(commits) < 2 {
		return deny("COMMIT_MISSING", "no implementation commit after task start")
	}
	for i, c := range commits {
		parents, e := g.git("show", "-s", "--format=%P", c)
		if e != nil {
			return e
		}
		if len(strings.Fields(parents)) != 1 {
			return deny("NONLINEAR_HISTORY", c)
		}
		action := ""
		if i == 0 {
			action = "begin"
		}
		if e = g.trailers(c, t.ID, action); e != nil {
			return e
		}
		paths, e := g.paths(c)
		if e != nil {
			return e
		}
		if len(paths) == 0 {
			return deny("COMMIT_MISSING", "empty implementation commit")
		}
		if i == 0 {
			if e = g.metadataOnly(c, t.ID); e != nil {
				return e
			}
		} else {
			for _, p := range paths {
				if strings.HasPrefix(p, "docs/tasks/") {
					return deny("PLAN_CHANGED", "task metadata changed during implementation")
				}
			}
		}
	}
	initial, e := g.task(commits[0], t.ID)
	if e != nil {
		return e
	}
	if initial.Status != "active" || initial.Base != t.Base || initial.Previous != t.Previous || digest(initial.Plan) != digest(t.Plan) {
		return deny("PLAN_CHANGED", "plan differs from the start commit")
	}
	return nil
}
