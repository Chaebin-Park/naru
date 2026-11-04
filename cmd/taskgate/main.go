// taskgate is repository-local development tooling, not a naru subcommand.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Chaebin-Park/naru/internal/taskgate"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errout io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) == 0 {
		_, _ = fmt.Fprintln(errout, "usage: task-gate check|begin|resume|verify|close|bootstrap --task NARU-nnn [--plan file] [--json]; begin/close/bootstrap create metadata commits")
		return 64
	}
	f := flag.NewFlagSet("task-gate", flag.ContinueOnError)
	f.SetOutput(errout)
	id := f.String("task", "", "task ID")
	next := f.String("next", "", "successor for check")
	plan := f.String("plan", "", "public plan JSON for begin/bootstrap")
	asJSON := f.Bool("json", false, "emit JSON (always enabled)")
	_ = asJSON
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return 64
	}
	if *next != "" {
		if *id != "" {
			return 64
		}
		*id = *next
	}
	root, err := os.Getwd()
	if err != nil {
		return 70
	}
	g, err := taskgate.Open(ctx, root)
	if err != nil {
		r := taskgate.Result{Schema: 1, Action: args[0], Task: *id, Reasons: []string{"INSPECTION_ERROR"}, NextAction: err.Error()}
		if e := json.NewEncoder(out).Encode(r); e != nil {
			return 70
		}
		return 70
	}
	r, code := g.Execute(args[0], *id, *plan)
	if err = json.NewEncoder(out).Encode(r); err != nil {
		return 70
	}
	return code
}
