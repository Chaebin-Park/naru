package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/runlock"
	"github.com/Chaebin-Park/naru/internal/runner"
	"github.com/Chaebin-Park/naru/internal/surface"
)

func cmdBaseline(args []string, env Env) int {
	if len(args) == 0 || (args[0] != "extract" && args[0] != "diff" && args[0] != "approve") {
		_, _ = fmt.Fprintln(env.Stderr, "usage: naru baseline extract|diff|approve [--json] <surface>")
		return exitUsage
	}
	fs := newFlags("baseline "+args[0], env)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 1 {
		return exitUsage
	}
	root, cfg, code := project(env)
	if code != exitOK {
		return code
	}
	name := fs.Arg(0)
	if _, ok := cfg.Surfaces[name]; !ok {
		_, _ = fmt.Fprintf(env.Stderr, "naru: unknown surface %q\n", name)
		return exitUsage
	}
	if args[0] == "approve" {
		return approveBaseline(root, name, *asJSON, env)
	}
	if args[0] == "extract" {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		snapshot, err := runner.Extract(root, name, ctx)
		if err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
			switch {
			case errors.Is(err, baseline.ErrStorage):
				return exitInternal
			case errors.Is(err, runner.ErrInterrupted):
				return exitInterrupted
			case errors.Is(err, runner.ErrEmptyInputs):
				return exitUsage
			case errors.Is(err, surface.ErrExtraction), errors.Is(err, runlock.ErrBusy), errors.Is(err, runner.ErrRecovery):
				return exitNotDone
			default:
				return exitInternal
			}
		}
		if *asJSON {
			if err := json.NewEncoder(env.Stdout).Encode(snapshot); err != nil {
				return exitInternal
			}
		} else {
			_, _ = fmt.Fprintf(env.Stdout, "CANDIDATE %s %s; run naru baseline diff %s\n", name, snapshot.Digest, name)
		}
		return exitOK
	}
	report, err := baseline.Diff(root, name)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
		if errors.Is(err, baseline.ErrNotReady) || errors.Is(err, baseline.ErrStale) || errors.Is(err, runlock.ErrBusy) {
			return exitNotDone
		}
		return exitInternal
	}
	if *asJSON {
		if err := json.NewEncoder(env.Stdout).Encode(report); err != nil {
			return exitInternal
		}
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "%s %s: +%d -%d ~%d\n", report.State, name, len(report.Added), len(report.Removed), len(report.Modified))
		for _, item := range report.Added {
			_, _ = fmt.Fprintf(env.Stdout, "+ %s %s\n", item.ID, item.Value)
		}
		for _, item := range report.Removed {
			_, _ = fmt.Fprintf(env.Stdout, "- %s %s\n", item.ID, item.Value)
		}
		for _, item := range report.Modified {
			_, _ = fmt.Fprintf(env.Stdout, "~ %s %s -> %s\n", item.ID, item.Before, item.After)
		}
	}
	if report.State != "unchanged" {
		return exitNotDone
	}
	return exitOK
}
