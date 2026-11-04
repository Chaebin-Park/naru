package taskgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func verificationEnv() []string {
	var env []string
	for _, item := range os.Environ() {
		key := strings.SplitN(item, "=", 2)[0]
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "NARU_") || key == "CLAUDE_PROJECT_DIR" || key == "GOTOOLCHAIN" {
			continue
		}
		env = append(env, item)
	}
	return append(env, "GOTOOLCHAIN=local")
}
func (g *Gate) verify(id string) (retErr error) {
	if err := g.clean(); err != nil {
		return err
	}
	w, head, err := g.state()
	if err != nil {
		return err
	}
	if w.Active != id && (w.Active != "" || w.Completed != id) {
		return deny("ACTIVE_TASK_MISMATCH", "verify active or last completed task only")
	}
	t, err := g.task(head, id)
	if err != nil {
		return err
	}
	tip := head
	if t.Status == "completed" {
		tip = t.Implementation
	}
	if err = g.implementation(t, tip); err != nil {
		return err
	}
	// A new attempt replaces prior authority before any fallible validation work.
	if err = os.Remove(g.evidencePath(tip)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.MkdirTemp("", "naru-task-verify-")
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, os.RemoveAll(tmp)) }()
	work := filepath.Join(tmp, "work")
	if _, err = g.git("clone", "--quiet", "--no-hardlinks", "--no-checkout", "--", g.Root, work); err != nil {
		return err
	}
	isolated := &Gate{Root: work, ctx: g.ctx}
	if _, err = isolated.git("checkout", "--quiet", "--detach", tip); err != nil {
		return err
	}
	run, err := os.MkdirTemp(filepath.Join(g.local(), "runs"), "verify-")
	if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(filepath.Join(g.local(), "runs"), 0o700); err != nil {
			return err
		}
		run, err = os.MkdirTemp(filepath.Join(g.local(), "runs"), "verify-")
	}
	if err != nil {
		return err
	}
	tree, err := g.git("rev-parse", tip+"^{tree}")
	if err != nil {
		return err
	}
	e := Evidence{Schema: Version, Task: id, Commit: tip, Tree: strings.TrimSpace(tree), Root: g.Root, Plan: digest(t.Plan), Profile: Profile, Created: time.Now().UTC().Format(time.RFC3339Nano), Steps: []Step{}, Tests: []string{}}
	if g.verifyRun != nil {
		err = g.verifyRun(work, run, &e)
	} else {
		err = g.runProfile(work, run, &e)
	}
	if err != nil {
		return err
	}
	if err = g.clean(); err != nil {
		return err
	}
	after, err := g.head()
	if err != nil {
		return err
	}
	if after != head {
		return deny("HEAD_CHANGED", "implementation changed while verification ran")
	}
	if err = writeJSON(g.evidencePath(tip), e); err != nil {
		return err
	}
	return g.evidence(t, tip)
}
func (g *Gate) runProfile(work, run string, e *Evidence) error {
	// Copy only the existing fixed development tools. No installation or download setup.
	for _, tool := range []string{"golangci-lint", "govulncheck"} {
		p := filepath.Join(g.Root, ".naru/state/devtools/bin", tool)
		data, err := os.ReadFile(p) // #nosec G304 -- Fixed local tool names under the user-selected repository; these are trusted development executables.
		if err != nil {
			return fmt.Errorf("required local tool %s: %w", tool, err)
		}
		dest := filepath.Join(work, ".naru/state/devtools/bin", tool)
		if err = os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		if err = os.WriteFile(dest, data, 0o700); err != nil { // #nosec G306 G703 -- Fixed tool names in a fresh isolated checkout; copied executables need owner execute permission.
			return err
		}
	}
	bin := filepath.Join(filepath.Dir(work), "naru")
	commands := [][]string{{"gofmt", "-l", "."}, {"go", "vet", "./..."}, {"go", "test", "-race", "-count=1", "-json", "./..."}, {"go", "vet", "./..."}, {"go", "vet", "./..."}, {"go", "build", "-o", bin, "./cmd/naru"}, {bin, "check"}}
	for i, args := range commands {
		ctx, cancel := context.WithTimeout(g.ctx, 15*time.Minute)
		cmd := exec.CommandContext(ctx, args[0], args[1:]...) // #nosec G204 -- Fixed verification profile argv; binary is built from the explicitly selected trusted task commit.
		cmd.Dir = work
		cmd.Env = verificationEnv()
		if i == 3 || i == 4 {
			osname := "windows"
			if i == 4 {
				osname = "linux"
			}
			cmd.Env = append(cmd.Env, "GOOS="+osname)
		}
		cmd.WaitDelay = 5 * time.Second
		data, runErr := cmd.CombinedOutput()
		cancel()
		if err := g.recordStep(run, stepNames[i], data, e); err != nil {
			return err
		}
		if runErr != nil {
			return fmt.Errorf("%s failed (log %s): %w", stepNames[i], e.Steps[len(e.Steps)-1].Log, runErr)
		}
		if i == 0 && len(bytes.TrimSpace(data)) != 0 {
			return deny("VERIFICATION_FAILED", "gofmt reported unformatted files")
		}
		if i == 2 {
			tests, err := passingTests(data)
			if err != nil {
				return err
			}
			e.Tests = tests
		}
	}
	reports, err := filepath.Glob(filepath.Join(work, ".naru/state/devcheck-*/report.json"))
	if err != nil {
		return err
	}
	if len(reports) != 1 {
		return deny("VERIFICATION_FAILED", "expected exactly one fresh analysis report")
	}
	b, err := os.ReadFile(reports[0]) // #nosec G304 -- Analysis report is generated within the isolated verification checkout.
	if err != nil {
		return err
	}
	var report struct {
		Complete bool              `json:"complete"`
		Issues   []json.RawMessage `json:"issues"`
		Errors   []string          `json:"errors"`
	}
	if err = json.Unmarshal(b, &report); err != nil {
		return err
	}
	if !report.Complete || report.Issues == nil || report.Errors == nil || len(report.Issues) != 0 || len(report.Errors) != 0 {
		return deny("VERIFICATION_FAILED", "analysis incomplete or contains findings")
	}
	e.AnalysisComplete = true
	e.AnalysisIssues = len(report.Issues)
	e.AnalysisErrors = len(report.Errors)
	return g.recordStep(run, "analysis-report", b, e)
}
func (g *Gate) recordStep(run, name string, b []byte, e *Evidence) error {
	p := filepath.Join(run, name+".log")
	if err := os.WriteFile(p, b, 0o600); err != nil { // #nosec G703 -- Fixed profile step names under a generated private run directory.
		return err
	}
	rel, err := filepath.Rel(g.local(), p)
	if err != nil {
		return err
	}
	e.Steps = append(e.Steps, Step{name, 0, filepath.ToSlash(rel), digest(string(b))})
	return nil
}
func passingTests(data []byte) ([]string, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	passed := map[string]bool{}
	for {
		var row struct{ Action, Package, Test string }
		err := d.Decode(&row)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, invalid("invalid go test JSON")
		}
		key := row.Package + "/" + row.Test
		if row.Test != "" {
			switch row.Action {
			case "pass":
				passed[key] = true
			case "fail", "skip":
				passed[key] = false
			}
		}
	}
	var names []string
	for key, ok := range passed {
		if ok {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	return names, nil
}
