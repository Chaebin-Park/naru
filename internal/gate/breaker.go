package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/status"
)

// The breaker stops an endless block loop. An agent that cannot make progress
// (a denied permission, a missing tool) would otherwise be blocked on every
// stop; Claude Code's own cap resets whenever the agent calls a tool, so it does
// not end such loops. naru counts identical consecutive blocks within one stop
// chain and, past the limit, lets the agent stop while saying loudly that the
// work was not verified.

type breakerState struct {
	Fingerprint string `json:"fingerprint"`
	Repeats     int    `json:"repeats"`
}

func breakerPath(root string) string {
	return filepath.Join(root, config.Dir, "state", "gate.json")
}

func loadBreaker(root string) breakerState {
	var st breakerState
	if data, err := os.ReadFile(breakerPath(root)); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func saveBreaker(root string, st breakerState) {
	p := breakerPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	data, _ := json.Marshal(st)
	_ = os.WriteFile(p, data, 0o644)
}

func clearBreaker(root string) { _ = os.Remove(breakerPath(root)) }

// reportFingerprint changes whenever the agent makes progress: an input edit, a
// new run, or a different result.
func reportFingerprint(rep status.Report) string {
	h := sha256.New()
	fmt.Fprintf(h, "mode=%s\n", rep.Mode)
	for _, c := range rep.Checks {
		fmt.Fprintf(h, "%s|%s|%s", c.Check, c.State, c.CurrentHash)
		if c.Receipt != nil {
			fmt.Fprintf(h, "|%s|%s|%d", c.Receipt.FinishedAt, c.Receipt.Result, c.Receipt.ExitCode)
		}
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func textFingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// block emits msg and returns Block, unless the same block has now repeated
// more than maxRepeats times in one stop chain; then it returns Allow with a
// "not verified" notice.
func block(root string, in StopInput, maxRepeats int, fingerprint, msg string, stderr io.Writer) int {
	st := loadBreaker(root)
	if in.StopHookActive && st.Fingerprint == fingerprint {
		st.Repeats++
	} else {
		st = breakerState{Fingerprint: fingerprint, Repeats: 1}
	}
	if maxRepeats > 0 && st.Repeats > maxRepeats {
		clearBreaker(root)
		fmt.Fprintf(stderr, "naru: NOT VERIFIED — the same block repeated %d times with no progress, so naru is letting the agent stop.\n", maxRepeats)
		fmt.Fprint(stderr, msg)
		fmt.Fprintln(stderr, "A human must resolve this (see above). Set gate.max_repeats: 0 to never give up.")
		return Allow
	}
	saveBreaker(root, st)
	fmt.Fprint(stderr, msg)
	if maxRepeats > 0 && st.Repeats > 1 {
		fmt.Fprintf(stderr, "(identical block %d of %d; if you cannot make progress, say so and stop.)\n", st.Repeats, maxRepeats)
	}
	return Block
}
