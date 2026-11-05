package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/Chaebin-Park/naru/internal/policy"
	"github.com/Chaebin-Park/naru/internal/runlock"
	"github.com/Chaebin-Park/naru/internal/runner"
	"github.com/Chaebin-Park/naru/internal/surface"
)

func cmdPolicy(args []string, env Env) int {
	if len(args) == 0 {
		return policyUsage(env)
	}
	action := args[0]
	switch action {
	case "list", "test", "check", "bootstrap", "approve":
	default:
		return policyUsage(env)
	}
	// Accept the optional rule on either side of --json, without silently accepting extra arguments.
	parsed := append([]string(nil), args[1:]...)
	if action == "test" && len(parsed) > 0 && !strings.HasPrefix(parsed[0], "-") {
		parsed = append(parsed[1:], parsed[0])
	}
	flags := newFlags("policy "+action, env)
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(parsed); err != nil {
		return exitUsage
	}
	if (action != "test" && flags.NArg() != 0) || flags.NArg() > 1 {
		return policyUsage(env)
	}
	root, _, code := project(env)
	if code != exitOK {
		return code
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var value any
	switch action {
	case "list":
		index, err := policy.Load(root)
		if err != nil {
			return policyFailure(err, env)
		}
		if *asJSON {
			value = index
		} else {
			names := make([]string, 0, len(index.Rules))
			for name := range index.Rules {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				rule := index.Rules[name]
				_, _ = fmt.Fprintf(env.Stdout, "%s tool=%s source=%s fixtures=%d\n", name, rule.Tool, rule.Source, len(rule.Fixtures))
			}
			return exitOK
		}
	case "test":
		report, err := policy.TestFixtures(root, flags.Arg(0), ctx)
		if err != nil {
			return policyFailure(err, env)
		}
		value = report
		if !*asJSON {
			for _, result := range report.Results {
				state := "PASS"
				if !result.Passed {
					state = "FAIL"
				}
				_, _ = fmt.Fprintf(env.Stdout, "%s %s %s expect=%s %s\n", state, result.RuleID, result.Project, result.Expect, result.Error)
			}
		}
		if !report.Passed {
			code = exitNotDone
		}
	case "check":
		report, err := policy.Check(root, ctx)
		if err != nil {
			return policyFailure(err, env)
		}
		value = report
		if !*asJSON {
			_, _ = fmt.Fprintf(env.Stdout, "%s known=%d new=%d resolved=%d\n", report.State, len(report.Known), len(report.New), len(report.Resolved))
			if len(report.Known) > 0 {
				_, _ = fmt.Fprintln(env.Stderr, "naru: known policy findings remain; approval is required before shrinking the baseline")
			}
		}
		if report.State == "bootstrap-required" || len(report.New) > 0 {
			code = exitNotDone
		}
	case "bootstrap", "approve":
		input, ok := env.Stdin.(*os.File)
		if !ok || !isTerminal(input) || !terminalWriter(env.Stderr) {
			_, _ = fmt.Fprintln(env.Stderr, "naru: policy approval requires terminal stdin and stderr; no noninteractive approval is supported")
			return exitUsage
		}
		kind := "reduction"
		if action == "bootstrap" {
			kind = "bootstrap"
		}
		record, err := reviewPolicy(ctx, root, kind, env)
		if err != nil {
			return policyFailure(err, env)
		}
		value = record
		if !*asJSON {
			_, _ = fmt.Fprintf(env.Stdout, "POLICY BASELINE findings=%d approval=%s\n", len(record.Findings), record.Approval.Kind)
		}
	}
	if *asJSON {
		if err := json.NewEncoder(env.Stdout).Encode(value); err != nil {
			return exitInternal
		}
	}
	return code
}

func policyUsage(env Env) int {
	_, _ = fmt.Fprintln(env.Stderr, "usage: naru policy list|check|bootstrap|approve [--json]; naru policy test [rule-id] [--json]")
	return exitUsage
}
func policyFailure(err error, env Env) int {
	_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, runner.ErrInterrupted):
		return exitInterrupted
	case errors.Is(err, policy.ErrStorage):
		return exitInternal
	case errors.Is(err, policy.ErrNotReady), errors.Is(err, policy.ErrStale), errors.Is(err, policy.ErrIncompatible), errors.Is(err, policy.ErrGrowth), errors.Is(err, runlock.ErrBusy), errors.Is(err, runner.ErrRecovery), errors.Is(err, surface.ErrExtraction), errors.Is(err, errReviewCancelled):
		return exitNotDone
	case errors.Is(err, policy.ErrInvalid), errors.Is(err, runner.ErrEmptyInputs):
		return exitUsage
	default:
		return exitInternal
	}
}

func reviewPolicy(ctx context.Context, root, kind string, env Env) (*policy.Record, error) {
	review, err := policy.Preview(root, kind)
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(env.Stderr)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(review); err != nil {
		return nil, err
	}
	if review.Report.State == "unchanged" {
		if _, err := fmt.Fprintln(env.Stderr, "naru: policy baseline unchanged; no publication required"); err != nil {
			return nil, err
		}
		return policy.Approve(root, *review, "", "", ctx)
	}
	reader := bufio.NewReaderSize(env.Stdin, 2048)
	ask := func(prompt string) (string, error) {
		if _, err := fmt.Fprint(env.Stderr, prompt); err != nil {
			return "", err
		}
		type response struct {
			text string
			err  error
		}
		done := make(chan response, 1)
		go func() { line, err := reader.ReadSlice('\n'); done <- response{strings.TrimSpace(string(line)), err} }()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case reply := <-done:
			if reply.err != nil {
				return "", errReviewCancelled
			}
			return reply.text, nil
		}
	}
	reviewer, err := ask("Reviewer (self-reported): ")
	if err != nil {
		return nil, err
	}
	reason, err := ask("Reason: ")
	if err != nil {
		return nil, err
	}
	token := kind + " " + review.Report.CandidateDigest
	if len(review.Report.Known)+len(review.Report.New) == 0 {
		token = kind + " empty " + review.Report.CandidateDigest
	}
	confirmation, err := ask("Type '" + token + "' to publish: ")
	if err != nil {
		return nil, err
	}
	if confirmation != token {
		return nil, errReviewCancelled
	}
	return policy.Approve(root, *review, reviewer, reason, ctx)
}
