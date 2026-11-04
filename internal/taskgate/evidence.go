package taskgate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Step struct {
	Name string `json:"name"`
	Exit int    `json:"exit"`
	Log  string `json:"log"`
	Hash string `json:"hash"`
}
type Evidence struct {
	Schema           int      `json:"schema"`
	Task             string   `json:"task"`
	Commit           string   `json:"commit"`
	Tree             string   `json:"tree"`
	Root             string   `json:"root"`
	Plan             string   `json:"plan"`
	Profile          string   `json:"profile"`
	Created          string   `json:"created"`
	Steps            []Step   `json:"steps"`
	Tests            []string `json:"tests"`
	AnalysisComplete bool     `json:"analysis_complete"`
	AnalysisIssues   int      `json:"analysis_issues"`
	AnalysisErrors   int      `json:"analysis_errors"`
}

var stepNames = []string{"fmt", "vet", "race", "windows-vet", "linux-vet", "build", "naru-check", "analysis-report"}

func (g *Gate) local() string                  { return filepath.Join(g.Root, ".naru/state/taskgate") }
func (g *Gate) evidencePath(tip string) string { return filepath.Join(g.local(), tip+".json") }
func (g *Gate) loadEvidence(t Task, tip string) (Evidence, error) {
	var e Evidence
	if !sha.MatchString(tip) {
		return e, invalid("evidence commit")
	}
	b, err := os.ReadFile(g.evidencePath(tip)) // #nosec G304 -- Validated commit name in the selected local worktree's private evidence directory.
	if errors.Is(err, os.ErrNotExist) {
		return e, deny("VERIFICATION_MISSING", "run verify for "+t.ID)
	}
	if err != nil {
		return e, err
	}
	if err = decode(b, &e); err != nil {
		return e, err
	}
	tree, err := g.git("rev-parse", tip+"^{tree}")
	if err != nil {
		return e, err
	}
	if e.Schema != Version || e.Task != t.ID || e.Commit != tip || e.Tree != strings.TrimSpace(tree) || e.Root != g.Root || e.Plan != digest(t.Plan) || e.Profile != Profile {
		return e, deny("VERIFICATION_STALE", "evidence identity mismatch")
	}
	if _, err = time.Parse(time.RFC3339Nano, e.Created); err != nil {
		return e, invalid("verification timestamp")
	}
	if len(e.Steps) != len(stepNames) || !e.AnalysisComplete || e.AnalysisIssues != 0 || e.AnalysisErrors != 0 {
		return e, deny("VERIFICATION_FAILED", "incomplete verification")
	}
	for i, step := range e.Steps {
		if step.Name != stepNames[i] || step.Exit != 0 || !filepath.IsLocal(step.Log) || !strings.HasPrefix(step.Log, "runs/") {
			return e, deny("VERIFICATION_FAILED", "invalid step")
		}
		b, err := os.ReadFile(filepath.Join(g.local(), filepath.FromSlash(step.Log))) // #nosec G304 -- Local relative log paths are checked before joining the private evidence root.
		if err != nil {
			return e, deny("VERIFICATION_MISSING", "verification log unavailable")
		}
		if digest(string(b)) != step.Hash {
			return e, deny("VERIFICATION_STALE", "verification log changed")
		}
	}
	passed := map[string]bool{}
	for _, name := range e.Tests {
		passed[name] = true
	}
	for _, c := range t.Plan.Criteria {
		if !passed[c.Test] {
			return e, deny("ACCEPTANCE_PENDING", c.ID+": "+c.Test)
		}
	}
	return e, nil
}
func (g *Gate) evidence(t Task, tip string) error { _, err := g.loadEvidence(t, tip); return err }
func writeJSON(path string, v any) (retErr error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".taskgate-*")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); !errors.Is(err, os.ErrNotExist) {
			retErr = errors.Join(retErr, err)
		}
	}()
	_, w := f.Write(append(b, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(w, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
