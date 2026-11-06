// Package gate turns status reports into agent-hook decisions.
package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/mode"
	"github.com/Chaebin-Park/naru/internal/status"
)

// Exit codes for `naru gate stop`. 2 is what Claude Code's Stop hook treats as
// "block and feed stderr back to the model".
const (
	Allow = 0
	Block = 2
)

// DisableEnv, when set to "off" in the hook's environment, turns the gate into
// a no-op. It is a human escape hatch for environments naru cannot verify
// (for example a broken toolchain); it is reported on every stop.
const DisableEnv = "NARU_GATE"

// ProjectDirEnv is set by Claude Code for hooks. It is preferred over the
// payload cwd so an agent that cd's out of the project is still gated.
const ProjectDirEnv = "CLAUDE_PROJECT_DIR"

// StopInput is the subset of the Stop hook payload naru uses.
type StopInput struct {
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// Stop decides whether the agent may finish. It never runs checks itself; it
// only compares receipts with the current inputs, so it stays fast.
//
// The project is located from $CLAUDE_PROJECT_DIR, then the payload cwd, then
// fallbackDir; the first of these inside a naru project is gated. Outside any
// naru project it allows (naru must not trap unrelated sessions). A broken
// config blocks: an unverifiable state is not a passing state.
func Stop(stdin io.Reader, stdout, stderr io.Writer, fallbackDir string) int {
	if strings.EqualFold(os.Getenv(DisableEnv), "off") {
		if err := warnUser(stdout, stderr, fmt.Sprintf("naru: gate disabled by %s=off; completion was not verified", DisableEnv)); err != nil {
			return Block
		}
		return Allow
	}
	var in StopInput
	if data, err := io.ReadAll(stdin); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		_ = json.Unmarshal(data, &in) // tolerate unknown or partial payloads
	}
	root, err := findProject(os.Getenv(ProjectDirEnv), in.Cwd, fallbackDir)
	if errors.Is(err, config.ErrNotFound) {
		return Allow
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "naru: cannot locate project root: %v\n", err)
		return Block
	}
	cfg, err := config.Load(root)
	if err != nil {
		msg := fmt.Sprintf("naru: .naru/config.yaml is invalid, so completion cannot be verified:\n%v\nFix the config, then run `naru check`.\n", err)
		return block(root, in, config.DefaultMaxRepeats, textFingerprint(msg), msg, stdout, stderr)
	}
	m, src := mode.Resolve(root, cfg, "")
	rep, err := status.Evaluate(root, cfg, m)
	if err != nil {
		msg := fmt.Sprintf("naru: cannot evaluate mode %q (from %s): %v\n", m, src, err)
		if src == "stored" {
			msg += "The stored mode is no longer defined; run `naru mode --clear` or `naru mode <name>`.\n"
		}
		return block(root, in, cfg.GateMaxRepeats(), textFingerprint(msg), msg, stdout, stderr)
	}
	if rep.Satisfied {
		if err := errors.Join(clearBreaker(root), clearUnverified(root)); err != nil {
			_, _ = fmt.Fprintf(stderr, "naru: cannot clear gate state after verification: %v\n", err)
			return Block
		}
		if m != cfg.DefaultMode {
			// Surface a narrowed mode so the human sees it.
			if err := warnUser(stdout, stderr, fmt.Sprintf("naru: done for mode %q (selected via %s; default_mode is %q)", m, src, cfg.DefaultMode)); err != nil {
				return Block
			}
		}
		return Allow
	}
	return block(root, in, cfg.GateMaxRepeats(), reportFingerprint(rep), BlockMessage(rep), stdout, stderr)
}

func findProject(candidates ...string) (string, error) {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		root, err := config.FindRoot(c)
		if errors.Is(err, config.ErrNotFound) {
			continue
		}
		return root, err
	}
	return "", config.ErrNotFound
}

// BlockMessage explains, compactly, what must happen before finishing.
func BlockMessage(rep status.Report) string {
	var b strings.Builder // Builder writes cannot fail.
	var pending []string
	for _, c := range rep.Checks {
		if c.State != status.Pass {
			pending = append(pending, c.Check)
		}
	}
	_, _ = fmt.Fprintf(&b, "naru: not done — %d of %d required checks for mode %q are not satisfied for the current files:\n",
		len(pending), len(rep.Checks), rep.Mode)
	for _, c := range rep.Checks {
		if c.State != status.Pass {
			_, _ = fmt.Fprintf(&b, "  - %s: %s\n", c.Check, c.Message)
		}
	}
	_, _ = fmt.Fprintf(&b, "Run `naru check %s`, fix any failures, and finish only after it passes. Do not edit .naru/ to get past this.\n",
		strings.Join(pending, " "))
	for _, row := range rep.Surfaces {
		if row.State != "unchanged" {
			fmt.Fprintf(&b, "Surface %s: %s. Run `naru baseline extract %s`, then `naru baseline diff %s`; review changes before explicit approval.\n", row.Surface, row.State, row.Surface, row.Surface)
		}
	}
	return b.String()
}
