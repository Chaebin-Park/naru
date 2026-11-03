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
func Stop(stdin io.Reader, stderr io.Writer, fallbackDir string) int {
	if strings.EqualFold(os.Getenv(DisableEnv), "off") {
		fmt.Fprintf(stderr, "naru: gate disabled by %s=off; completion was not verified\n", DisableEnv)
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
		fmt.Fprintf(stderr, "naru: cannot locate project root: %v\n", err)
		return Block
	}
	cfg, err := config.Load(root)
	if err != nil {
		msg := fmt.Sprintf("naru: .naru/config.yaml is invalid, so completion cannot be verified:\n%v\nFix the config, then run `naru check`.\n", err)
		return block(root, in, config.DefaultMaxRepeats, textFingerprint(msg), msg, stderr)
	}
	m, src := mode.Resolve(root, cfg, "")
	rep, err := status.Evaluate(root, cfg, m)
	if err != nil {
		msg := fmt.Sprintf("naru: cannot evaluate mode %q (from %s): %v\n", m, src, err)
		if src == "stored" {
			msg += "The stored mode is no longer defined; run `naru mode --clear` or `naru mode <name>`.\n"
		}
		return block(root, in, cfg.GateMaxRepeats(), textFingerprint(msg), msg, stderr)
	}
	if rep.Satisfied {
		clearBreaker(root)
		if m != cfg.DefaultMode {
			// Surface a narrowed mode so a human reviewing the session can see it.
			fmt.Fprintf(stderr, "naru: done for mode %q (selected via %s; default_mode is %q)\n", m, src, cfg.DefaultMode)
		}
		return Allow
	}
	return block(root, in, cfg.GateMaxRepeats(), reportFingerprint(rep), BlockMessage(rep), stderr)
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
	var b strings.Builder
	var pending []string
	for _, c := range rep.Checks {
		if c.State != status.Pass {
			pending = append(pending, c.Check)
		}
	}
	fmt.Fprintf(&b, "naru: not done — %d of %d required checks for mode %q are not satisfied for the current files:\n",
		len(pending), len(rep.Checks), rep.Mode)
	for _, c := range rep.Checks {
		switch c.State {
		case status.Missing:
			fmt.Fprintf(&b, "  - %s: never run\n", c.Check)
		case status.Stale:
			fmt.Fprintf(&b, "  - %s: inputs or check definition changed since the last run\n", c.Check)
		case status.Fail:
			fmt.Fprintf(&b, "  - %s: %s (exit %d), log %s\n", c.Check, c.Receipt.Result, c.Receipt.ExitCode, c.Receipt.LogPath)
		case status.Empty:
			fmt.Fprintf(&b, "  - %s: its inputs match no files; a human must fix the patterns in .naru/config.yaml\n", c.Check)
		}
	}
	fmt.Fprintf(&b, "Run `naru check %s`, fix any failures, and finish only after it passes. Do not edit .naru/ to get past this.\n",
		strings.Join(pending, " "))
	return b.String()
}
