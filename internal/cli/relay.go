package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/relay"
)

type relayArtifacts []string

func (a *relayArtifacts) String() string         { return strings.Join(*a, ",") }
func (a *relayArtifacts) Set(value string) error { *a = append(*a, value); return nil }

func cmdRelay(args []string, env Env) int {
	if len(args) == 0 || (args[0] != "new" && args[0] != "submit") {
		return relayUsage(env)
	}
	action := args[0]
	parsed := append([]string(nil), args[1:]...)
	// Match the documented CHECK/ID-first form and the usual flags-first form.
	if len(parsed) > 0 && !strings.HasPrefix(parsed[0], "-") {
		parsed = append(parsed[1:], parsed[0])
	}
	flags := newFlags("relay "+action, env)
	asJSON := flags.Bool("json", false, "emit metadata as JSON without artifact contents")
	var target, file string
	var artifacts relayArtifacts
	var ttl time.Duration
	if action == "new" {
		flags.StringVar(&target, "target", "", "description of the environment where the human will run the check")
		flags.Var(&artifacts, "artifact", "required artifact name; may repeat")
		flags.DurationVar(&ttl, "expires-in", 24*time.Hour, "evidence validity duration (maximum 168h)")
	} else {
		flags.StringVar(&file, "file", "", "submission JSON file")
	}
	if err := flags.Parse(parsed); err != nil {
		return exitUsage
	}
	if flags.NArg() != 1 || (action == "submit" && file == "") || (action == "new" && ttl <= 0) {
		return relayUsage(env)
	}
	root, _, code := project(env)
	if code != exitOK {
		return code
	}
	parent := env.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var value any
	if action == "new" {
		packet, err := relay.New(root, flags.Arg(0), relay.NewOptions{Context: ctx, Target: target, Artifacts: artifacts, TTL: ttl})
		if err != nil {
			return relayFailure(err, env)
		}
		hash, err := receipt.PacketHash(packet)
		if err != nil {
			return relayFailure(err, env)
		}
		packetPath := filepath.Join(root, ".naru", "relay", "packets", packet.PacketID+".json")
		value = struct {
			Packet     receipt.RelayPacket `json:"packet"`
			PacketHash string              `json:"packet_hash"`
			PacketPath string              `json:"packet_path"`
		}{packet, hash, packetPath}
		if !*asJSON {
			_, _ = fmt.Fprintf(env.Stdout, "Relay request %s for %s expires %s.\n", packet.PacketID, packet.Check, packet.ExpiresAt.Format(time.RFC3339))
			_, _ = fmt.Fprintf(env.Stdout, "Packet hash: %s\nPacket: %s\n", hash, packetPath)
			_, _ = fmt.Fprintln(env.Stdout, "This check is pending and earlier evidence no longer satisfies it. Run the packet command yourself against the requested inputs, then submit the result. NARU did not execute it.")
		}
	} else {
		if !filepath.IsAbs(file) {
			file = filepath.Join(env.Dir, file)
		}
		record, err := relay.Submit(root, flags.Arg(0), file, relay.SubmitOptions{Context: ctx})
		if err != nil {
			return relayFailure(err, env)
		}
		value = record
		if record.Acceptance == nil {
			return relayFailure(errors.New("accepted relay record has no result"), env)
		}
		if record.Acceptance.Result != receipt.ResultPass {
			code = exitNotDone
		}
		if !*asJSON {
			_, _ = fmt.Fprintf(env.Stdout, "%s %s (human-relayed, packet %s)\n", record.Acceptance.Result, record.Check, record.Packet.PacketID)
		}
	}
	if *asJSON {
		if err := json.NewEncoder(env.Stdout).Encode(value); err != nil {
			return relayFailure(err, env)
		}
	}
	return code
}

func relayFailure(err error, env Env) int {
	_, _ = fmt.Fprintf(env.Stderr, "naru: %v\n", err)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return exitInterrupted
	case errors.Is(err, relay.ErrNotReady):
		return exitNotDone
	case errors.Is(err, relay.ErrInvalid):
		return exitUsage
	default:
		return exitInternal
	}
}

func relayUsage(env Env) int {
	_, _ = fmt.Fprintln(env.Stderr, "usage: naru relay new CHECK --target TEXT --artifact NAME [--artifact NAME...] [--expires-in DURATION] [--json]\n       naru relay submit PACKET_ID --file SUBMISSION.json [--json]\nIssuing a request supersedes this check's earlier evidence with pending state. Relay never executes the command.")
	return exitUsage
}
