package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const candidatePath = ".naru/state/policy/candidate.json"
const baselinePath = ".naru/policy-baseline.json"

type candidate struct {
	Schema           int       `json:"schema"`
	Phase            string    `json:"phase"`
	Reason           string    `json:"reason"`
	DefinitionDigest string    `json:"definition_digest"`
	SurfaceID        string    `json:"surface_id"`
	SnapshotDigest   string    `json:"snapshot_digest"`
	InputDigest      string    `json:"input_digest"`
	Findings         []Finding `json:"findings"`
}

func validateCandidate(c candidate) bool {
	if c.Schema != 1 {
		return false
	}
	if c.Phase != "ready" {
		return (c.Phase == "running" && c.Reason == "" || c.Phase == "failed" && text(c.Reason, 512)) && c.DefinitionDigest == "" && c.SurfaceID == "" && c.SnapshotDigest == "" && c.InputDigest == "" && c.Findings == nil
	}
	return c.Reason == "" && validDigest(c.DefinitionDigest) && text(c.SurfaceID, 128) && validDigest(c.SnapshotDigest) && validDigest(c.InputDigest) && sortedFindings(c.Findings)
}
func readState(root, rel string) ([]byte, error) {
	p, e := safePath(root, rel, true)
	if e != nil {
		return nil, e
	}
	return readBounded(p, 8<<20)
}
func loadCandidate(root string) (*candidate, string, error) {
	data, e := readState(root, candidatePath)
	if errors.Is(e, os.ErrNotExist) {
		return nil, "", ErrNotReady
	}
	if e != nil {
		return nil, "", e
	}
	if e = fields(data, "schema", "phase", "reason", "definition_digest", "surface_id", "snapshot_digest", "input_digest", "findings"); e != nil {
		return nil, "", e
	}
	var c candidate
	if e = decode(data, &c); e != nil {
		return nil, "", e
	}
	if !validateCandidate(c) {
		return nil, "", ErrInvalid
	}
	if c.Phase != "ready" {
		return nil, "", ErrNotReady
	}
	return &c, hash(data), nil
}
func loadRecord(root string) (*Record, string, error) {
	data, e := readState(root, baselinePath)
	if errors.Is(e, os.ErrNotExist) {
		return nil, "", nil
	}
	if e != nil {
		return nil, "", e
	}
	if e = fields(data, "schema", "definition_digest", "findings", "approval"); e != nil {
		return nil, "", e
	}
	var record Record
	if e = decode(data, &record); e != nil {
		return nil, "", e
	}
	a := record.Approval
	stamp, e := time.Parse(time.RFC3339Nano, a.ApprovedAt)
	if record.Schema != 1 || !validDigest(record.DefinitionDigest) || !sortedFindings(record.Findings) || !validDigest(a.CandidateDigest) || !validDigest(a.InputDigest) || !text(a.Reviewer, 128) || !text(a.Reason, 1024) || e != nil || stamp.Location() != time.UTC {
		return nil, "", ErrInvalid
	}
	if a.Kind == "bootstrap" {
		if a.PreviousRevision != nil {
			return nil, "", ErrInvalid
		}
	} else if a.Kind != "reduction" || a.PreviousRevision == nil || !validDigest(*a.PreviousRevision) {
		return nil, "", ErrInvalid
	}
	// Enforce presence of nullable and nested fields as well as unknown-field rejection.
	var envelope map[string]json.RawMessage
	if e = json.Unmarshal(data, &envelope); e != nil {
		return nil, "", e
	}
	if e = fields(envelope["approval"], "kind", "previous_revision", "candidate_digest", "input_digest", "reviewer", "reason", "approved_at"); e != nil {
		return nil, "", e
	}
	return &record, hash(data), nil
}
func writeCandidate(root string, c candidate) error {
	if !validateCandidate(c) {
		return ErrInvalid
	}
	return writeJSON(context.Background(), root, candidatePath, c)
}
func writeJSON(ctx context.Context, root, rel string, v any) error {
	p, e := safePath(root, rel, true)
	if e != nil {
		return fmt.Errorf("%w: %w", ErrStorage, e)
	}
	data, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if e = publish(p, data, func(from, to string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		return os.Rename(from, to)
	}); e != nil {
		return fmt.Errorf("%w: %w", ErrStorage, e)
	}
	return nil
}

// publish keeps the previous record until a complete synced temporary file is renamed.
func publish(path string, data []byte, rename func(string, string) error) (retErr error) {
	if e := os.MkdirAll(filepath.Dir(path), 0o700); e != nil {
		return e
	} // #nosec G703 -- Internal policy state path validated before publication.
	f, e := os.CreateTemp(filepath.Dir(path), ".policy-*.tmp")
	if e != nil {
		return e
	}
	closed, published := false, false
	defer func() {
		if !closed {
			retErr = errors.Join(retErr, f.Close())
		}
		if !published {
			if e := os.Remove(f.Name()); !errors.Is(e, os.ErrNotExist) {
				retErr = errors.Join(retErr, e)
			}
		}
	}()
	n, e := f.Write(data)
	if e != nil {
		return e
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	if e = f.Sync(); e != nil {
		return e
	}
	closed = true
	if e = f.Close(); e != nil {
		return e
	}
	if e = rename(f.Name(), path); e != nil {
		return e
	}
	published = true
	return nil
}
