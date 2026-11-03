// Package runner executes checks and records receipts.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
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
}

// waitDelay bounds how long to wait for output after the command exits or is killed.
var waitDelay = 5 * time.Second

// ErrEmptyInputs means a check's inputs match no files. Such a receipt would
// depend only on the command and stay "pass" forever, so naru refuses to run it.
var ErrEmptyInputs = errors.New("inputs match no files")

// ErrInterrupted means the run was cancelled before finishing; no receipt is written.
var ErrInterrupted = errors.New("interrupted")

// Outcome is the result of running one check.
type Outcome struct {
	Receipt receipt.Receipt
	// Tail holds the last lines of output, for failure summaries.
	Tail string
}

// Run executes one check, writes its log and receipt, and returns the outcome.
// The inputs digest is taken before the command runs, so edits made while the
// check runs leave the receipt stale rather than falsely fresh.
func Run(root string, cfg *config.Config, name string, opt Options) (Outcome, error) {
	ch, ok := cfg.Checks[name]
	if !ok {
		return Outcome{}, fmt.Errorf("unknown check %q", name)
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	digest, err := fileset.Compute(root, ch.Inputs, cfg.Exclude, cfg.Fingerprint(name))
	if err != nil {
		return Outcome{}, fmt.Errorf("check %q: hashing inputs: %w", name, err)
	}
	if digest.FileCount == 0 {
		return Outcome{}, fmt.Errorf("check %q: %w (inputs %v); fix the patterns in .naru/config.yaml", name, ErrEmptyInputs, ch.Inputs)
	}
	parent := opt.Context
	if parent == nil {
		parent = context.Background()
	}

	start := now()
	logRel := filepath.ToSlash(filepath.Join(".naru", "logs", fmt.Sprintf("%s-%s.log", name, start.UTC().Format("20060102T150405.000Z"))))
	logPath := filepath.Join(root, filepath.FromSlash(logRel))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return Outcome{}, err
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return Outcome{}, err
	}
	defer logFile.Close()

	var tail tailBuffer
	writers := []io.Writer{logFile, &tail}
	if opt.Stream != nil {
		writers = append(writers, opt.Stream)
	}
	out := io.MultiWriter(writers...)

	ctx, cancel := context.WithTimeout(parent, ch.TimeoutDuration())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", ch.Run)
	cmd.Dir = root
	if ch.Workdir != "" {
		cmd.Dir = filepath.Join(root, filepath.FromSlash(ch.Workdir))
	}
	cmd.Env = append(os.Environ(), "NARU_CHECK="+name, "NARU_ROOT="+root)
	cmd.Stdout = out
	cmd.Stderr = out
	setProcessGroup(cmd)
	// If a descendant escapes the process group and keeps the output pipe open,
	// stop waiting for it shortly after the kill instead of hanging forever.
	cmd.WaitDelay = waitDelay

	result, exitCode := receipt.ResultPass, 0
	runErr := cmd.Run()
	if parent.Err() != nil {
		fmt.Fprintf(out, "\nnaru: check %q interrupted\n", name)
		return Outcome{}, fmt.Errorf("check %q: %w", name, ErrInterrupted)
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		result, exitCode = receipt.ResultTimeout, -1
		fmt.Fprintf(out, "\nnaru: check %q timed out after %s\n", name, ch.TimeoutDuration())
	case errors.Is(runErr, exec.ErrWaitDelay):
		// The command exited, but a background child kept its output open.
		// Its exit status is still the result.
		exitCode = cmd.ProcessState.ExitCode()
		if exitCode != 0 {
			result = receipt.ResultFail
		}
		fmt.Fprintf(out, "\nnaru: check %q exited %d but a background process kept its output open; it was not waited for\n", name, exitCode)
	case runErr != nil:
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			result, exitCode = receipt.ResultFail, ee.ExitCode()
		} else {
			result, exitCode = receipt.ResultError, -1
			fmt.Fprintf(out, "\nnaru: could not start check %q: %v\n", name, runErr)
		}
	}
	end := now()

	r := receipt.Receipt{
		Schema:      receipt.Schema,
		Check:       name,
		Command:     ch.Run,
		InputsHash:  digest.Hash,
		FileCount:   digest.FileCount,
		FileSource:  string(digest.Source),
		GitHead:     fileset.GitHead(root),
		Result:      result,
		ExitCode:    exitCode,
		Evidence:    opt.Evidence,
		StartedAt:   start.UTC(),
		FinishedAt:  end.UTC(),
		DurationMS:  end.Sub(start).Milliseconds(),
		LogPath:     logRel,
		NaruVersion: opt.Version,
	}
	if err := receipt.NewStore(root).Put(r); err != nil {
		return Outcome{}, fmt.Errorf("check %q: writing receipt: %w", name, err)
	}
	return Outcome{Receipt: r, Tail: tail.String()}, nil
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
