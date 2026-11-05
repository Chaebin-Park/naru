// Package relay records human-reported execution without running remote commands.
package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

var ErrInvalid = errors.New("invalid relay request")
var ErrNotReady = errors.New("relay evidence not ready")

type NewOptions struct {
	Target    string
	Artifacts []string
	TTL       time.Duration
	Now       func() time.Time
	Context   context.Context
}
type SubmitOptions struct {
	Now     func() time.Time
	Context context.Context
}

func relayContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
func relayNow(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}
func identity() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func validID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func acquire(root, check string) (*runlock.Lock, error) {
	if !config.NamePattern.MatchString(check) {
		return nil, fmt.Errorf("%w: check name", ErrInvalid)
	}
	lock, err := runlock.Acquire(root, check)
	if errors.Is(err, runlock.ErrBusy) {
		return nil, fmt.Errorf("%w: %v", ErrNotReady, err)
	}
	return lock, err
}
func load(root, check string) (*config.Config, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, ok := cfg.Checks[check]; !ok {
		return nil, fmt.Errorf("%w: unknown check", ErrInvalid)
	}
	return cfg, nil
}
func inputDigest(root, check string, cfg *config.Config) (receipt.InputDigest, error) {
	d, err := fileset.Compute(root, cfg.Checks[check].Inputs, cfg.Exclude, cfg.Fingerprint(check))
	if err != nil {
		return receipt.InputDigest{}, fmt.Errorf("%w: input read: %v", ErrNotReady, err)
	}
	if d.FileCount == 0 {
		return receipt.InputDigest{}, fmt.Errorf("%w: empty inputs", ErrNotReady)
	}
	return receipt.InputDigest{Hash: d.Hash, FileCount: d.FileCount, FileSource: string(d.Source)}, nil
}
func replaceable(store receipt.StateStore, check string) error {
	e, err := store.CurrentEvidence(check)
	if err != nil {
		return currentError(err)
	}
	if e.Record != nil && (e.Record.Phase == receipt.PhaseRunning || e.Record.Inputs.Reason != nil && *e.Record.Inputs.Reason == "owner_lost") {
		return fmt.Errorf("%w: local execution requires recovery", ErrNotReady)
	}
	return nil
}

func currentError(err error) error {
	if errors.Is(err, receipt.ErrInvalid) || errors.Is(err, receipt.ErrUnsupported) {
		return fmt.Errorf("%w: current evidence: %v", ErrNotReady, err)
	}
	return err
}

func New(root, check string, options NewOptions) (packet receipt.RelayPacket, retErr error) {
	ctx := relayContext(options.Context)
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	ttl := options.TTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	if ttl < 0 || ttl > 7*24*time.Hour {
		return packet, fmt.Errorf("%w: expiry must be positive and at most 7d", ErrInvalid)
	}
	lock, err := acquire(root, check)
	if err != nil {
		return packet, err
	}
	defer func() { retErr = errors.Join(retErr, lock.Close()) }()
	root = lock.Root
	cfg, err := load(root, check)
	if err != nil {
		return packet, err
	}
	store := receipt.NewStateStore(root)
	if err := replaceable(store, check); err != nil {
		return packet, err
	}
	d, err := inputDigest(root, check, cfg)
	if err != nil {
		return packet, err
	}
	id, err := identity()
	if err != nil {
		return packet, err
	}
	now := relayNow(options.Now)
	ch := cfg.Checks[check]
	wd := ch.Workdir
	if wd == "" {
		wd = "."
	}
	packet = receipt.RelayPacket{Schema: 1, PacketID: id, Check: check, IssuedAt: now, ExpiresAt: now.Add(ttl), InputDigest: d, DefinitionHash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(cfg.Fingerprint(check)))), Command: ch.Run, Workdir: wd, TimeoutMS: ch.TimeoutDuration().Milliseconds(), Inputs: append([]string{}, ch.Inputs...), Excludes: append([]string{}, cfg.Exclude...), Requirements: receipt.RelayRequirements{Tools: append([]string{}, ch.Requires.Tools...), OS: append([]string{}, ch.Requires.OS...)}, Target: options.Target, ExpectedArtifacts: append([]string{}, options.Artifacts...)}
	hash, err := receipt.PacketHash(packet)
	if err != nil {
		return packet, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	if err := store.ExportRelayPacket(packet); err != nil {
		return packet, err
	}
	if err := ctx.Err(); err != nil {
		return packet, err
	}
	r := receipt.RelayRecord{Schema: receipt.RelaySchema, InputPolicy: receipt.RelayInputPolicy, Check: check, RunID: id, Phase: receipt.PhasePending, Provenance: receipt.Provenance{Kind: receipt.EvidenceHumanRelayed}, Packet: packet, PacketHash: hash}
	return packet, store.BeginRelay(r)
}

func Submit(root, packetID, submissionPath string, options SubmitOptions) (record receipt.RelayRecord, retErr error) {
	ctx := relayContext(options.Context)
	if err := ctx.Err(); err != nil {
		return record, err
	}
	if !validID(packetID) {
		return record, fmt.Errorf("%w: packet identity", ErrInvalid)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return record, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return record, err
	}
	path := filepath.Join(root, ".naru", "relay", "packets", packetID+".json")
	data, err := receipt.ReadRelayFile(root, path, receipt.RelayJSONLimit)
	if err != nil {
		return record, fmt.Errorf("%w: packet: %v", ErrNotReady, err)
	}
	packet, err := receipt.DecodeRelayPacket(data)
	if err != nil {
		return record, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if packet.PacketID != packetID {
		return record, fmt.Errorf("%w: packet identity", ErrInvalid)
	}
	lock, err := acquire(root, packet.Check)
	if err != nil {
		return record, err
	}
	defer func() { retErr = errors.Join(retErr, lock.Close()) }()
	root = lock.Root
	store := receipt.NewStateStore(root)
	e, err := store.CurrentEvidence(packet.Check)
	if err != nil {
		return record, currentError(err)
	}
	hash, err := receipt.PacketHash(*packet)
	if err != nil {
		return record, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if e.Relay == nil || e.Relay.Phase != receipt.PhasePending || e.Relay.RunID != packetID || e.Relay.PacketHash != hash {
		return record, fmt.Errorf("%w: request is not current pending authority", ErrNotReady)
	}
	record = *e.Relay
	if err := fresh(root, *packet, relayNow(options.Now)); err != nil {
		return record, err
	}
	submissionPath, err = filepath.Abs(submissionPath)
	if err != nil {
		return record, err
	}
	sourceDir := filepath.Dir(submissionPath)
	data, err = receipt.ReadRelayFile(sourceDir, submissionPath, receipt.RelayJSONLimit)
	if err != nil {
		return record, fmt.Errorf("%w: submission file: %v", ErrInvalid, err)
	}
	submission, err := receipt.DecodeRelaySubmission(data)
	if err != nil {
		return record, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if submission.PacketID != packetID || submission.PacketHash != hash || submission.Check != packet.Check {
		return record, fmt.Errorf("%w: submission identity", ErrInvalid)
	}
	now := relayNow(options.Now)
	if submission.StartedAt.Before(packet.IssuedAt) || submission.FinishedAt.After(now) || submission.BeforeInputDigest != packet.InputDigest || submission.AfterInputDigest != packet.InputDigest {
		return record, fmt.Errorf("%w: submission times or inputs", ErrNotReady)
	}
	artifacts, err := copyArtifacts(ctx, root, sourceDir, *packet, *submission)
	if err != nil {
		return record, err
	}
	now = relayNow(options.Now)
	if err := ctx.Err(); err != nil {
		return record, err
	}
	if err := fresh(root, *packet, now); err != nil {
		return record, err
	}
	now = relayNow(options.Now)
	if now.Before(submission.FinishedAt) || !now.Before(packet.ExpiresAt) {
		return record, fmt.Errorf("%w: acceptance clock or expiry", ErrNotReady)
	}
	record.Phase = receipt.PhaseTerminal
	record.Acceptance = &receipt.RelayAcceptance{AcceptedAt: now, SubmittedBy: submission.SubmittedBy, StartedAt: submission.StartedAt, FinishedAt: submission.FinishedAt, BeforeInputDigest: submission.BeforeInputDigest, AfterInputDigest: submission.AfterInputDigest, Result: submission.Result, ExitCode: submission.ExitCode, ToolVersions: submission.ToolVersions, Artifacts: artifacts}
	if err := record.Validate(packet.Check); err != nil {
		return record, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		return record, err
	}
	return record, store.PutRelay(record)
}
func fresh(root string, p receipt.RelayPacket, now time.Time) error {
	if now.Before(p.IssuedAt) || !now.Before(p.ExpiresAt) {
		return fmt.Errorf("%w: request expired or clock moved backwards", ErrNotReady)
	}
	cfg, err := load(root, p.Check)
	if err != nil {
		return fmt.Errorf("%w: definition unavailable: %v", ErrNotReady, err)
	}
	if !receipt.RelayDefinitionMatches(p, cfg) {
		return fmt.Errorf("%w: definition changed", ErrNotReady)
	}
	d, err := inputDigest(root, p.Check, cfg)
	if err != nil {
		return err
	}
	if d != p.InputDigest {
		return fmt.Errorf("%w: inputs changed", ErrNotReady)
	}
	return nil
}

func safeArtifactSource(dir, path string) (string, error) {
	if !filepath.IsLocal(path) || strings.Contains(path, "\\") {
		return "", fmt.Errorf("%w: artifact path must stay beneath submission directory", ErrNotReady)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: artifact traversal", ErrNotReady)
		}
	}
	result := filepath.Join(dir, path)
	if err := receipt.RelaySafePath(dir, result); err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotReady, err)
	}
	return result, nil
}
