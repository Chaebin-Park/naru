package runner

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/receipt"
)

type faultyLog struct {
	*os.File
	failure string
	closed  bool
}

var errLog = errors.New("injected log failure")

func (f *faultyLog) Write(p []byte) (int, error) {
	if f.failure == "header" || (f.failure == "footer" && strings.Contains(string(p), "naru: result")) {
		return 0, errLog
	}
	return f.File.Write(p)
}
func (f *faultyLog) Sync() error {
	if f.failure == "sync" {
		return errLog
	}
	return f.File.Sync()
}
func (f *faultyLog) Close() error {
	f.closed = true
	err := f.File.Close()
	if f.failure == "close" {
		return errors.Join(err, errLog)
	}
	return err
}
func TestLogFailureCannotPublishPass(t *testing.T) {
	for _, failure := range []string{"header", "footer", "sync", "close"} {
		t.Run(failure, func(t *testing.T) {
			root, cfg := project(t, base+`"true"`)
			var sink *faultyLog
			_, err := runWithLog(root, cfg, "c", Options{}, func(path string) (logSink, error) {
				f, err := os.Create(path) // #nosec G304 -- Reads a synthetic fixture or a file in the test-owned temporary directory.
				if err != nil {
					return nil, err
				}
				sink = &faultyLog{File: f, failure: failure}
				return sink, nil
			})
			if !errors.Is(err, errLog) {
				t.Errorf("expected log error, got %v", err)
			}
			if sink == nil || !sink.closed {
				t.Error("log not closed")
			}
			r, err := receipt.NewStateStore(root).Current("c")
			if err != nil {
				t.Fatal(err)
			}
			if r != nil && r.Phase == receipt.PhaseTerminal {
				t.Fatalf("published receipt despite log failure: %+v", r)
			}
		})
	}
}
