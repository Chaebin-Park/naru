package gate

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/status"
)

// PrePush checks the default mode without the Stop hook's escape hatches.
// It evaluates the working tree, not the commits sent to the remote.
func PrePush(dir string, stderr io.Writer) int {
	root, err := config.FindRoot(dir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "naru: push blocked: cannot locate project: %v; run `naru init`\n", err)
		return 1
	}
	cfg, err := config.Load(root)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "naru: push blocked: invalid config: %v\n", err)
		return 1
	}
	blocked := false
	// Lstat also detects broken symlinks. Never treat malformed state as absent.
	if _, err := os.Lstat(unverifiedPath(root)); !errors.Is(err, os.ErrNotExist) {
		blocked = true
		_, _ = fmt.Fprintln(stderr, "naru: push blocked: NOT VERIFIED record exists or cannot be inspected.")
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
		}
		_, _ = fmt.Fprintf(stderr, "Run `naru check --mode %s`, then a verified `naru gate stop` with NARU_MODE=%s and NARU_GATE unset, from this project, to clear the record.\n", cfg.DefaultMode, cfg.DefaultMode)
	}
	rep, err := status.Evaluate(root, cfg, cfg.DefaultMode)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "naru: push blocked: cannot evaluate default_mode: %v\n", err)
		return 1
	}
	if !rep.Satisfied {
		blocked = true
		_, _ = fmt.Fprint(stderr, BlockMessage(rep))
	}
	if blocked {
		return 1
	}
	return 0
}
