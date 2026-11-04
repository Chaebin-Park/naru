package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

func terminalWriter(w io.Writer) bool {
	if wrapped, ok := w.(*outputWriter); ok {
		w = wrapped.Writer
	}
	f, ok := w.(*os.File)
	return ok && isTerminal(f)
}

func approveBaseline(root, name string, asJSON bool, env Env) int {
	input, ok := env.Stdin.(*os.File)
	if !ok || !isTerminal(input) || !terminalWriter(env.Stderr) {
		_, _ = fmt.Fprintln(env.Stderr, "naru: approval requires terminal stdin and stderr; no noninteractive approval is supported")
		return exitUsage
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	record, err := reviewBaseline(ctx, root, name, env)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
		switch {
		case errors.Is(err, context.Canceled):
			return exitInterrupted
		case errors.Is(err, baseline.ErrStorage):
			return exitInternal
		case errors.Is(err, baseline.ErrStale), errors.Is(err, baseline.ErrNotReady), errors.Is(err, runlock.ErrBusy), errors.Is(err, errReviewCancelled):
			return exitNotDone
		default:
			return exitInternal
		}
	}
	if asJSON {
		if err := json.NewEncoder(env.Stdout).Encode(record); err != nil {
			return exitInternal
		}
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "APPROVED %s %s\n", name, record.Snapshot.Digest)
	}
	return exitOK
}

var errReviewCancelled = errors.New("approval cancelled; baseline unchanged")

func reviewBaseline(ctx context.Context, root, name string, env Env) (*baseline.Record, error) {
	report, err := baseline.Diff(root, name)
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(env.Stderr)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return nil, err
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
		result := make(chan response, 1)
		go func() {
			line, err := reader.ReadSlice('\n')
			if len(line) > 2048 {
				err = errReviewCancelled
			}
			result <- response{strings.TrimSpace(string(line)), err}
		}()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case r := <-result:
			if r.err != nil {
				return "", errReviewCancelled
			}
			return r.text, nil
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
	token := "approve " + report.NewDigest
	if report.CandidateCount == 0 {
		token = "approve empty " + report.NewDigest
	}
	confirmation, err := ask("Type '" + token + "' to publish: ")
	if err != nil {
		return nil, err
	}
	if confirmation != token {
		return nil, errReviewCancelled
	}
	return baseline.Approve(ctx, root, name, *report, reviewer, reason)
}
