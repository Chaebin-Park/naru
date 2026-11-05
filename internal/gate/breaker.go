package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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

func saveBreaker(root string, st breakerState) error {
	p := breakerPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func removeState(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func clearBreaker(root string) error { return removeState(breakerPath(root)) }

// reportFingerprint changes whenever the agent makes progress: an input edit, a
// new run, or a different result.
func reportFingerprint(rep status.Report) string {
	h := sha256.New() // hash.Hash.Write never returns an error.
	_, _ = fmt.Fprintf(h, "mode=%s\n", rep.Mode)
	for _, c := range rep.Checks {
		_, _ = fmt.Fprintf(h, "%s|%s|%s", c.Check, c.State, c.CurrentHash)
		if c.Record != nil {
			_, _ = fmt.Fprintf(h, "|%s|%s|%s", c.Record.RunID, c.Record.Phase, c.Reason)
		}
		if c.Receipt != nil {
			_, _ = fmt.Fprintf(h, "|%s|%s|%d", c.Receipt.FinishedAt, c.Receipt.Result, c.Receipt.ExitCode)
		}
		if c.Relay != nil {
			_, _ = fmt.Fprintf(h, "|relay:%s|%s", c.Relay.PacketID, c.Reason)
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
// "not verified" notice. Diagnostic write errors on blocking paths cannot
// turn a block into an allowance; allowance notices must be delivered.
func block(root string, in StopInput, maxRepeats int, fingerprint, msg string, stdout, stderr io.Writer) int {
	st := loadBreaker(root)
	if in.StopHookActive && st.Fingerprint == fingerprint {
		st.Repeats++
	} else {
		st = breakerState{Fingerprint: fingerprint, Repeats: 1}
	}
	if maxRepeats > 0 && st.Repeats > maxRepeats {
		if err := recordUnverified(root, msg); err != nil {
			_, _ = fmt.Fprintf(stderr, "naru: cannot save NOT VERIFIED record: %v\n", err)
			return Block
		}
		if err := clearBreaker(root); err != nil {
			_, _ = fmt.Fprintf(stderr, "naru: cannot clear gate state: %v\n", err)
			return Block
		}
		head := fmt.Sprintf("naru: NOT VERIFIED — the same block repeated %d times with no progress, so naru let the agent stop.", maxRepeats)
		if err := warnUser(stdout, stderr, head+"\n"+strings.TrimRight(msg, "\n")+"\nA human must resolve this; `naru status` keeps showing it until a verified stop. Set gate.max_repeats: 0 to never give up."); err != nil {
			return Block
		}
		return Allow
	}
	if err := saveBreaker(root, st); err != nil {
		_, _ = fmt.Fprintf(stderr, "naru: cannot save gate state: %v\n", err)
	}
	_, _ = fmt.Fprint(stderr, msg)
	if maxRepeats > 0 && st.Repeats > 1 {
		_, _ = fmt.Fprintf(stderr, "(identical block %d of %d; if you cannot make progress, say so and stop.)\n", st.Repeats, maxRepeats)
	}
	return Block
}

// warnUser shows msg to the human: Claude Code displays a hook's
// {"systemMessage": ...} on stdout (with exit 0) to the user. The same text
// goes to stderr for other agents and for logs.
func warnUser(stdout, stderr io.Writer, msg string) error {
	b, _ := json.Marshal(map[string]string{"systemMessage": msg})
	_, outErr := fmt.Fprintln(stdout, string(b))
	_, errErr := fmt.Fprintln(stderr, msg)
	return errors.Join(outErr, errErr)
}

// Unverified is the record of a stop naru allowed without verification.
type Unverified struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

func unverifiedPath(root string) string {
	return filepath.Join(root, config.Dir, "state", "unverified.json")
}

func recordUnverified(root, reason string) error {
	p := unverifiedPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(Unverified{At: time.Now().UTC(), Reason: reason})
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func clearUnverified(root string) error { return removeState(unverifiedPath(root)) }

// LastUnverified returns the record of the last stop allowed without
// verification, or nil when the latest stop was verified (or none happened).
func LastUnverified(root string) *Unverified {
	data, err := os.ReadFile(unverifiedPath(root))
	if err != nil {
		return nil
	}
	var u Unverified
	if json.Unmarshal(data, &u) != nil {
		return nil
	}
	return &u
}
