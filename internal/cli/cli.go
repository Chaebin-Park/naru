// Package cli implements the naru command line.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/gate"
	"github.com/Chaebin-Park/naru/internal/hooks"
	"github.com/Chaebin-Park/naru/internal/initcmd"
	"github.com/Chaebin-Park/naru/internal/mode"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runlock"
	"github.com/Chaebin-Park/naru/internal/runner"
	"github.com/Chaebin-Park/naru/internal/status"
)

// Version is set at release build time with
// -ldflags "-X github.com/Chaebin-Park/naru/internal/cli.Version=v0.1.0".
// `go install ...@v0.1.0` leaves it empty, so the module version is used.
var Version = ""

func version() string {
	if Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// Exit codes (except `gate`, which follows the hook protocol).
const (
	exitOK          = 0
	exitNotDone     = 1 // checks failed or are not satisfied
	exitUsage       = 64
	exitInternal    = 70
	exitInterrupted = 130
)

// Env lets tests inject I/O and the working directory.
type Env struct {
	Context context.Context // optional parent cancellation for relay commands
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Dir     string
}

// Main runs naru with os-level I/O.
func Main(args []string) int {
	wd, _ := os.Getwd()
	return Run(args, Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Dir: wd})
}

const usage = `naru — verify that an AI agent's work is actually done.

Usage:
  naru init [--force]                 create .naru/config.yaml for this repository
  naru check [--mode M] [check...]    run checks and record receipts
  naru status [--mode M] [--json | --markdown]     show whether required checks pass for the current files
  naru mode [name | --clear]          show or set the mode for this working copy
  naru gate stop                      agent Stop hook: exit 2 with reasons unless satisfied
  naru gate pre-tool                  agent PreToolUse hook: block edits to .naru/ and mode changes
  naru gate pre-push                  require fresh default_mode receipts and no NOT VERIFIED record
  naru hooks install [--force]        install a Git pre-push hook (preserve existing hooks by default)
  naru baseline extract|diff|approve [--json] <surface>  extract a candidate or compare it to a baseline
  naru policy list|test|check|bootstrap|approve [--json]  inspect policies and approve violation reductions
  naru relay new|submit [--json]      request and submit human-executed check evidence
  naru version

Exit codes: 0 done, 1 not done, 64 usage/config error, 70 internal error, 130 interrupted.
`

// Run dispatches a subcommand.
func Run(args []string, env Env) (code int) {
	stdout, stderr := &outputWriter{Writer: env.Stdout}, &outputWriter{Writer: env.Stderr}
	env.Stdout, env.Stderr = stdout, stderr
	defer func() {
		if code == exitOK && (stdout.err != nil || stderr.err != nil) {
			code = exitInternal
			if len(args) > 1 && args[0] == "gate" {
				code = gate.Block
				if args[1] == "pre-push" {
					code = exitNotDone
				}
			}
		}
	}()
	if len(args) == 0 {
		_, _ = fmt.Fprint(env.Stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "init":
		return cmdInit(args[1:], env)
	case "check":
		return cmdCheck(args[1:], env)
	case "status":
		return cmdStatus(args[1:], env)
	case "mode":
		return cmdMode(args[1:], env)
	case "gate":
		return cmdGate(args[1:], env)
	case "hooks":
		return cmdHooks(args[1:], env)
	case "baseline":
		return cmdBaseline(args[1:], env)
	case "relay":
		return cmdRelay(args[1:], env)
	case "policy":
		return cmdPolicy(args[1:], env)
	case "version", "--version":
		_, _ = fmt.Fprintln(env.Stdout, "naru", version())
		return exitOK
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(env.Stdout, usage)
		return exitOK
	default:
		_, _ = fmt.Fprintf(env.Stderr, "naru: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

func newFlags(name string, env Env) *flag.FlagSet {
	fs := flag.NewFlagSet("naru "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs
}

// project locates the root and loads its config, printing a helpful error.
func project(env Env) (string, *config.Config, int) {
	root, err := config.FindRoot(env.Dir)
	if errors.Is(err, config.ErrNotFound) {
		_, _ = fmt.Fprintln(env.Stderr, "naru: no .naru/config.yaml found here or in any parent directory; run `naru init`")
		return "", nil, exitUsage
	}
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
		return "", nil, exitInternal
	}
	cfg, err := config.Load(root)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: invalid .naru/config.yaml:\n%v\n", err)
		return "", nil, exitUsage
	}
	return root, cfg, exitOK
}

func cmdInit(args []string, env Env) int {
	fs := newFlags("init", env)
	force := fs.Bool("force", false, "overwrite an existing .naru/config.yaml")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	res, err := initcmd.Run(env.Dir, *force)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
		if res.Existing {
			return exitUsage
		}
		return exitInternal
	}
	for _, f := range res.Created {
		_, _ = fmt.Fprintf(env.Stdout, "created %s\n", f)
	}
	if res.Stack == "" {
		_, _ = fmt.Fprintln(env.Stdout, "no known build file found; the placeholder check fails until you configure one")
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "detected %s; review the `test` check, then run `naru check`\n", res.Stack)
	}
	return exitOK
}

func cmdCheck(args []string, env Env) int {
	fs := newFlags("check", env)
	modeFlag := fs.String("mode", "", "run the checks of this mode (default: current mode)")
	verbose := fs.Bool("verbose", false, "stream command output")
	evidence := fs.String("evidence", "", "evidence class to record: local, agent-run or ci (default: $NARU_EVIDENCE, ci when $CI is set, else local)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, cfg, code := project(env)
	if code != exitOK {
		return code
	}
	names := fs.Args()
	if len(names) == 0 {
		m, _ := mode.Resolve(root, cfg, *modeFlag)
		var err error
		if names, err = cfg.ModeChecks(m); err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
			return exitUsage
		}
	}
	for _, n := range names {
		if _, ok := cfg.Checks[n]; !ok {
			_, _ = fmt.Fprintf(env.Stderr, "naru: unknown check %q\n", n)
			return exitUsage
		}
	}
	ev, err := resolveEvidence(*evidence)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opt := runner.Options{Evidence: ev, Version: version(), Context: ctx}
	if *verbose {
		opt.Stream = env.Stderr
	}
	failed := 0
	for _, n := range names {
		out, err := runner.Run(root, cfg, n, opt)
		switch {
		case errors.Is(err, runlock.ErrBusy), errors.Is(err, runner.ErrRecovery):
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
			return exitNotDone
		case errors.Is(err, runner.ErrEmptyInputs):
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
			return exitUsage
		case errors.Is(err, runner.ErrInterrupted):
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v; any started execution is recorded as cancelled\n", err)
			return exitInterrupted
		case err != nil:
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
			return exitInternal
		}
		r := out.Receipt
		dur := (time.Duration(r.DurationMS) * time.Millisecond).Round(10 * time.Millisecond)
		if r.Result == receipt.ResultPass && out.Record.Inputs.Validity == receipt.InputsMatched {
			_, _ = fmt.Fprintf(env.Stdout, "PASS %s (%s, %d files)\n", n, dur, r.FileCount)
			continue
		}
		failed++
		if r.Result == receipt.ResultNotRun {
			_, _ = fmt.Fprintf(env.Stdout, "NOT-RUN %s: %s\n", n, out.Record.Execution.Unavailable.Message())
			continue
		}
		if out.Record.Inputs.Validity != receipt.InputsMatched {
			_, _ = fmt.Fprintf(env.Stdout, "INPUT-%s %s; command result %s; rerun after resolving the input change or read failure\n", strings.ToUpper(out.Record.Inputs.Validity), n, r.Result)
		}
		_, _ = fmt.Fprintf(env.Stdout, "%s %s exit=%d (%s) log: %s\n", strings.ToUpper(r.Result), n, r.ExitCode, dur, r.LogPath)
		if !*verbose && out.Tail != "" {
			for _, line := range strings.Split(out.Tail, "\n") {
				_, _ = fmt.Fprintf(env.Stdout, "    %s\n", line)
			}
		}
	}
	if failed > 0 {
		_, _ = fmt.Fprintf(env.Stdout, "%d of %d checks did not pass\n", failed, len(names))
		return exitNotDone
	}
	return exitOK
}

func resolveEvidence(flagVal string) (string, error) {
	v := flagVal
	if v == "" {
		v = os.Getenv("NARU_EVIDENCE")
	}
	if v == "" {
		if os.Getenv("CI") != "" {
			return receipt.EvidenceCI, nil
		}
		return receipt.EvidenceLocal, nil
	}
	switch v {
	case receipt.EvidenceLocal, receipt.EvidenceAgent, receipt.EvidenceCI:
		return v, nil
	}
	return "", fmt.Errorf("unknown evidence class %q (use local, agent-run or ci)", v)
}

func cmdStatus(args []string, env Env) int {
	fs := newFlags("status", env)
	modeFlag := fs.String("mode", "", "mode to evaluate (default: current mode)")
	asJSON := fs.Bool("json", false, "print a JSON report")
	asMarkdown := fs.Bool("markdown", false, "print a Markdown report")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *asJSON && *asMarkdown {
		_, _ = fmt.Fprintln(env.Stderr, "naru: choose either --json or --markdown")
		return exitUsage
	}
	root, cfg, code := project(env)
	if code != exitOK {
		return code
	}
	m, src := mode.Resolve(root, cfg, *modeFlag)
	if _, err := cfg.ModeChecks(m); err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
		return exitUsage
	}
	rep, err := status.Evaluate(root, cfg, m)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
		return exitInternal
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return exitInternal
		}
	} else if *asMarkdown {
		if err := status.WriteMarkdown(env.Stdout, rep); err != nil {
			return exitInternal
		}
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "mode %s (%s)\n", m, src)
		for _, c := range rep.Checks {
			line := fmt.Sprintf("  %-7s %s", c.State, c.Check)
			if c.Message != "" {
				line += "  " + c.Message
			}
			if c.Record != nil && c.Record.FinishedAt != nil {
				line += fmt.Sprintf(" (last run %s, %s)", c.Record.FinishedAt.Local().Format("2006-01-02 15:04"), c.Record.Provenance.Kind)
			}
			if c.Receipt != nil && c.State != status.Missing {
				line += fmt.Sprintf("  (last run %s, %s)", c.Receipt.FinishedAt.Local().Format("2006-01-02 15:04"), c.Receipt.Evidence)
			}
			_, _ = fmt.Fprintln(env.Stdout, line)
		}
		for _, row := range rep.Surfaces {
			_, _ = fmt.Fprintf(env.Stdout, "  surface %s: %s\n", row.Surface, row.State)
		}
		if rep.Unverified.Present {
			_, _ = fmt.Fprintln(env.Stdout, "warning: NOT VERIFIED record exists or cannot be read; completion was allowed WITHOUT verification")
		}
		if rep.Satisfied {
			_, _ = fmt.Fprintln(env.Stdout, "done: every required check and selected baseline passes for the current files")
		} else {
			_, _ = fmt.Fprintln(env.Stdout, "not done: rerun failing checks; extract and review required surface baselines")
		}
	}
	if !rep.Satisfied {
		return exitNotDone
	}
	return exitOK
}

func cmdMode(args []string, env Env) int {
	fs := newFlags("mode", env)
	clear := fs.Bool("clear", false, "forget the stored mode and use default_mode")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	root, cfg, code := project(env)
	if code != exitOK {
		return code
	}
	switch {
	case *clear:
		if err := mode.Clear(root); err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
			return exitInternal
		}
		_, _ = fmt.Fprintf(env.Stdout, "mode cleared; using default_mode %s\n", cfg.DefaultMode)
	case fs.NArg() == 1:
		name := fs.Arg(0)
		if _, err := cfg.ModeChecks(name); err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
			return exitUsage
		}
		if err := mode.Store(root, name); err != nil {
			_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
			return exitInternal
		}
		_, _ = fmt.Fprintf(env.Stdout, "mode set to %s: requires %s\n", name, strings.Join(cfg.Modes[name], ", "))
	case fs.NArg() == 0:
		m, src := mode.Resolve(root, cfg, "")
		_, _ = fmt.Fprintf(env.Stdout, "%s (%s)\n", m, src)
	default:
		_, _ = fmt.Fprintln(env.Stderr, "naru: usage: naru mode [name | --clear]")
		return exitUsage
	}
	return exitOK
}

func cmdGate(args []string, env Env) int {
	if len(args) == 1 && args[0] == "pre-push" {
		return gate.PrePush(env.Dir, env.Stderr)
	}
	if len(args) == 1 && args[0] == "stop" {
		return gate.Stop(env.Stdin, env.Stdout, env.Stderr, env.Dir)
	}
	if len(args) == 1 && args[0] == "pre-tool" {
		return gate.PreTool(env.Stdin, env.Stderr, env.Dir)
	}
	_, _ = fmt.Fprintln(env.Stderr, "naru: usage: naru gate stop | naru gate pre-tool | naru gate pre-push")
	return exitUsage
}

func cmdHooks(args []string, env Env) int {
	if len(args) == 0 || args[0] != "install" {
		_, _ = fmt.Fprintln(env.Stderr, "naru: usage: naru hooks install [--force]")
		return exitUsage
	}
	fs := newFlags("hooks install", env)
	force := fs.Bool("force", false, "replace an existing regular pre-push hook")
	if err := fs.Parse(args[1:]); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(env.Stderr, "naru: usage: naru hooks install [--force]")
		return exitUsage
	}
	root, _, code := project(env)
	if code != exitOK {
		return code
	}
	p, err := hooks.Install(root, *force)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
		return exitNotDone
	}
	_, _ = fmt.Fprintf(env.Stdout, "installed %s; keep naru on PATH (linked worktrees share Git hooks)\n", p)
	return exitOK
}

// outputWriter retains output errors for Run's exit-code boundary. Formatting
// calls below can discard their immediate errors because this writer keeps them.
type outputWriter struct {
	io.Writer
	err error
}

func (w *outputWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.Writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}
