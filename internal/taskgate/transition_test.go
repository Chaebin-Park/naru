package taskgate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/runlock"
)

func successfulProfile(g *Gate) func(string, string, *Evidence) error {
	return func(work, run string, e *Evidence) error {
		// The production verifier is exercised separately against this repository.
		// This seam tests state/commit transitions without recursively testing the suite.
		if _, err := os.Stat(filepath.Join(work, "input.txt")); err != nil {
			return err
		}
		for _, name := range stepNames {
			if err := g.recordStep(run, name, []byte("fixture result\n"), e); err != nil {
				return err
			}
		}
		e.Tests = []string{"example.test/app/TestRequired"}
		e.AnalysisComplete = true
		return nil
	}
}
func executeTest(t *testing.T, g *Gate, action, id, path string, want int) Result {
	t.Helper()
	r, code := g.Execute(action, id, path)
	if code != want || r.Allowed != (want == 0) {
		t.Fatalf("%s: code=%d %+v", action, code, r)
	}
	return r
}
func setupActive(t *testing.T) (*Gate, string) {
	t.Helper()
	g := fixture(t)
	p := filepath.Join(t.TempDir(), "plan.json")
	b := planFixture()
	jsonTest(t, g, "unused", b)
	// Plan input is outside the repository so it cannot bypass the clean-tree check.
	data, err := os.ReadFile(filepath.Join(g.Root, "unused"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(g.Root, "unused")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, data, 0o600); err != nil { // #nosec G703 -- Explicit plan fixture in this test-owned temporary directory.
		t.Fatal(err)
	}
	executeTest(t, g, "bootstrap", "NARU-100", p, 0)
	g.verifyRun = successfulProfile(g)
	return g, p
}
func TestTaskTransitionLifecycle(t *testing.T) {
	g, p := setupActive(t)
	executeTest(t, g, "begin", "NARU-101", p, 1)
	implementationTest(t, g)
	executeTest(t, g, "close", "NARU-100", "", 1)
	executeTest(t, g, "verify", "NARU-100", "", 0)
	executeTest(t, g, "close", "NARU-100", "", 0)
	executeTest(t, g, "check", "NARU-101", "", 0)
	executeTest(t, g, "begin", "NARU-101", p, 0)
	executeTest(t, g, "resume", "NARU-101", "", 0)
	executeTest(t, g, "resume", "NARU-100", "", 1)
}
func TestVerificationBindsCommitAndRequiredTests(t *testing.T) {
	g, _ := setupActive(t)
	implementationTest(t, g)
	g.verifyRun = func(w, r string, e *Evidence) error {
		if err := successfulProfile(g)(w, r, e); err != nil {
			return err
		}
		e.Tests = []string{}
		return nil
	}
	r := executeTest(t, g, "verify", "NARU-100", "", 1)
	if r.Reasons[0] != "ACCEPTANCE_PENDING" {
		t.Fatal(r)
	}
	g.verifyRun = successfulProfile(g)
	executeTest(t, g, "verify", "NARU-100", "", 0)
	writeTest(t, g.Root, "input.txt", "new implementation")
	gitTest(t, g, "add", "input.txt")
	gitTest(t, g, "commit", "-qm", "change\n\nTask: NARU-100")
	r = executeTest(t, g, "close", "NARU-100", "", 1)
	if r.Reasons[0] != "VERIFICATION_MISSING" {
		t.Fatal(r)
	}
}
func TestEvidenceLossAndLogTampering(t *testing.T) {
	g, _ := setupActive(t)
	tip := implementationTest(t, g)
	executeTest(t, g, "verify", "NARU-100", "", 0)
	task, err := g.task("HEAD", "NARU-100")
	if err != nil {
		t.Fatal(err)
	}
	ev, err := g.loadEvidence(task, tip)
	if err != nil {
		t.Fatal(err)
	}
	writeTest(t, g.local(), ev.Steps[0].Log, "changed")
	r := executeTest(t, g, "close", "NARU-100", "", 1)
	if r.Reasons[0] != "VERIFICATION_STALE" {
		t.Fatal(r)
	}
	executeTest(t, g, "verify", "NARU-100", "", 0)
	executeTest(t, g, "close", "NARU-100", "", 0)
	if err = os.Remove(g.evidencePath(tip)); err != nil {
		t.Fatal(err)
	}
	executeTest(t, g, "check", "NARU-101", "", 1)
	executeTest(t, g, "verify", "NARU-100", "", 0)
	executeTest(t, g, "check", "NARU-101", "", 0)
}
func TestFailureNeverPublishesCompletion(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "cancelled"}[cancel], func(t *testing.T) {
			g, _ := setupActive(t)
			implementationTest(t, g)
			g.verifyRun = func(string, string, *Evidence) error {
				if cancel {
					return context.Canceled
				}
				return errors.New("required check failed")
			}
			want := 70
			if cancel {
				want = 130
			}
			executeTest(t, g, "verify", "NARU-100", "", want)
			executeTest(t, g, "close", "NARU-100", "", 1)
			executeTest(t, g, "check", "NARU-101", "", 1)
		})
	}
}
func TestCommitFailureRetainsBlockedState(t *testing.T) {
	g, _ := setupActive(t)
	implementationTest(t, g)
	executeTest(t, g, "verify", "NARU-100", "", 0)
	// A Git lock causes commit to fail without installing or changing hooks.
	writeTest(t, g.Root, ".git/index.lock", "")
	executeTest(t, g, "close", "NARU-100", "", 70)
	if err := os.Remove(filepath.Join(g.Root, ".git/index.lock")); err != nil {
		t.Fatal(err)
	}
	executeTest(t, g, "check", "NARU-101", "", 1)
	w, err := g.workflow()
	if err != nil || w.Active != "NARU-100" {
		t.Fatalf("%+v %v", w, err)
	}
}
func TestTransitionLockAndDeletedLedger(t *testing.T) {
	g, p := setupActive(t)
	lock, err := runlock.Acquire(g.local(), "transition")
	if err != nil {
		t.Fatal(err)
	}
	r := executeTest(t, g, "resume", "NARU-100", "", 1)
	if r.Reasons[0] != "TRANSITION_BUSY" {
		t.Fatal(r)
	}
	if err = lock.Close(); err != nil {
		t.Fatal(err)
	}
	gitTest(t, g, "rm", ledgerPath)
	gitTest(t, g, "commit", "-qm", "delete ledger\n\nTask: NARU-100")
	r = executeTest(t, g, "bootstrap", "NARU-101", p, 1)
	if r.Reasons[0] != "WORKFLOW_ALREADY_INITIALIZED" {
		t.Fatal(r)
	}
}
func TestVerificationRefusesConcurrentChanges(t *testing.T) {
	g, _ := setupActive(t)
	implementationTest(t, g)
	g.verifyRun = func(w, r string, e *Evidence) error {
		if err := successfulProfile(g)(w, r, e); err != nil {
			return err
		}
		writeTest(t, g.Root, "input.txt", "concurrent edit")
		return nil
	}
	executeTest(t, g, "verify", "NARU-100", "", 1)
}
func TestRequiredSkippedTestIsNotPassed(t *testing.T) {
	tests, err := passingTests([]byte("{\"Action\":\"skip\",\"Package\":\"example.test/app\",\"Test\":\"TestRequired\"}\n"))
	if err != nil || len(tests) != 0 {
		t.Fatalf("%v %v", tests, err)
	}
	if _, err = passingTests([]byte("broken")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
func TestCommittedPlanCannotBeRelaxed(t *testing.T) {
	g, _ := setupActive(t)
	implementationTest(t, g)
	task, err := g.task("HEAD", "NARU-100")
	if err != nil {
		t.Fatal(err)
	}
	task.Plan.Criteria[0].Description = "relaxed"
	jsonTest(t, g, taskPath(task.ID), task)
	gitTest(t, g, "add", taskPath(task.ID))
	gitTest(t, g, "commit", "-qm", "relax\n\nTask: NARU-100")
	r := executeTest(t, g, "verify", "NARU-100", "", 1)
	if !strings.Contains(r.NextAction, "PLAN_CHANGED") {
		t.Fatal(r)
	}
}

func TestFailedReverificationInvalidatesPriorSuccess(t *testing.T) {
	g, _ := setupActive(t)
	implementationTest(t, g)
	executeTest(t, g, "verify", "NARU-100", "", 0)
	g.verifyRun = func(string, string, *Evidence) error { return errors.New("new verification failed") }
	executeTest(t, g, "verify", "NARU-100", "", 70)
	executeTest(t, g, "close", "NARU-100", "", 1)
}

func TestConcurrentBeginStartsOnlyOneTask(t *testing.T) {
	g, p := setupActive(t)
	implementationTest(t, g)
	executeTest(t, g, "verify", "NARU-100", "", 0)
	executeTest(t, g, "close", "NARU-100", "", 0)
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, id := range []string{"NARU-101", "NARU-102"} {
		go func(id string) { <-start; _, code := g.Execute("begin", id, p); results <- code }(id)
	}
	close(start)
	a, b := <-results, <-results
	validPair := (a == 0 && b == 1) || (a == 1 && b == 0)
	if !validPair {
		t.Fatalf("expected one success and one denial: %d %d", a, b)
	}
}

func TestCancelledGitInspectionReturnsInterrupted(t *testing.T) {
	g, _ := setupActive(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g.ctx = ctx
	executeTest(t, g, "resume", "NARU-100", "", 130)
}
