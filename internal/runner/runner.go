// Package runner executes checks and records receipts.
package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/receipt"
)

// Options control a run.
type Options struct {
	Evidence string    // receipt evidence class
	Version  string    // naru version stamped on receipts
	Stream   io.Writer // when non-nil, command output is also streamed here
	Now      func() time.Time
	// Context cancels the run (e.g. on Ctrl-C); nil means context.Background().
	Context context.Context
	store   func(string) executionStore
}

// waitDelay bounds how long to wait for output after the command exits or is killed.
var waitDelay = 5 * time.Second

// ErrEmptyInputs means a check's inputs match no files. Such a receipt would
// depend only on the command and stay "pass" forever, so naru refuses to run it.
var ErrEmptyInputs = errors.New("inputs match no files")

// ErrInterrupted means the caller cancelled; a started execution records cancellation.
var ErrInterrupted = errors.New("interrupted")

// Outcome is the result of running one check.
type Outcome struct {
	Receipt receipt.Receipt
	Record  *receipt.Record
	// Tail holds the last lines of output, for failure summaries.
	Tail string
}

// Run executes under a check lock and publishes before/after execution evidence.
func Run(root string, cfg *config.Config, name string, opt Options) (Outcome, error) {
	return runWithLog(root, cfg, name, opt, func(path string) (logSink, error) {
		return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- Generated check/run ID path under the canonical project root.
	})
}

type logSink interface {
	io.WriteCloser
	Sync() error
}

func runWithLog(root string, cfg *config.Config, name string, opt Options, openLog func(string) (logSink, error)) (Outcome, error) {
	return runState(root, cfg, name, opt, openLog)
}

// tailBuffer keeps roughly the last 8 KiB of output.
type tailBuffer struct{ buf bytes.Buffer }

const tailLimit = 8 << 10

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if t.buf.Len() > 2*tailLimit {
		b := t.buf.Bytes()
		keep := append([]byte(nil), b[len(b)-tailLimit:]...)
		t.buf.Reset()
		t.buf.Write(keep)
	}
	return len(p), nil
}

// String returns the last 40 lines.
func (t *tailBuffer) String() string {
	lines := strings.Split(strings.TrimRight(t.buf.String(), "\n"), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return strings.Join(lines, "\n")
}

// checkedOutput keeps command-copy and diagnostic errors until completion.
// os/exec serializes writes when Stdout and Stderr share this writer.
type checkedOutput struct {
	io.Writer
	err error
}

func (w *checkedOutput) Write(p []byte) (int, error) {
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
