package cli

import (
	"errors"
	"io"
	"testing"
)

type brokenOutput struct{}

func (brokenOutput) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }
func TestSuccessfulCommandsRejectOutputFailure(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"help"}} {
		if got := Run(args, Env{Stdout: brokenOutput{}, Stderr: io.Discard}); got != exitInternal {
			t.Errorf("%v: exit %d", args, got)
		}
	}
}
