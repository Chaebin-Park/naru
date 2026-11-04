package baseline

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/Chaebin-Park/naru/internal/surface"
)

// Approval records a self-reported review, not an authenticated human identity.
type Approval struct {
	ID          string  `json:"id"`
	OldDigest   *string `json:"old_digest"`
	NewDigest   string  `json:"new_digest"`
	InputDigest string  `json:"input_digest"`
	Reviewer    string  `json:"reviewer"`
	Reason      string  `json:"reason"`
	ApprovedAt  string  `json:"approved_at"`
}

// Record keeps the snapshot and approval in the same atomic publication.
type Record struct {
	Schema   int               `json:"schema"`
	Snapshot *surface.Snapshot `json:"snapshot"`
	Approval *Approval         `json:"approval"`
}

func object(data []byte, keys ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalid
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		key, ok := token.(string)
		if !ok || fields[key] != nil {
			return nil, ErrInvalid
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return nil, ErrInvalid
		}
		fields[key] = raw
	}
	if _, err := d.Token(); err != nil {
		return nil, ErrInvalid
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}
	if len(fields) != len(keys) {
		return nil, ErrInvalid
	}
	for _, key := range keys {
		if fields[key] == nil {
			return nil, ErrInvalid
		}
	}
	return fields, nil
}

func digest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func reviewText(s string, limit int) bool {
	return strings.TrimSpace(s) == s && s != "" && len(s) <= limit && strings.IndexFunc(s, unicode.IsControl) < 0
}
func decodeRecord(data []byte, name string) (*Record, error) {
	// Bare snapshots remain readable, but carry no approval authority.
	if snapshot, err := surface.DecodeSnapshot(data, name); err == nil {
		return &Record{Schema: 1, Snapshot: snapshot}, nil
	}
	fields, err := object(data, "schema", "snapshot", "approval")
	if err != nil || string(fields["schema"]) != "1" {
		return nil, ErrInvalid
	}
	snapshot, err := surface.DecodeSnapshot(fields["snapshot"], name)
	if err != nil {
		return nil, err
	}
	if _, err := object(fields["approval"], "id", "old_digest", "new_digest", "input_digest", "reviewer", "reason", "approved_at"); err != nil {
		return nil, err
	}
	var a Approval
	if err := json.Unmarshal(fields["approval"], &a); err != nil {
		return nil, ErrInvalid
	}
	if !digest(a.ID) || (a.OldDigest != nil && !digest(*a.OldDigest)) || a.NewDigest != snapshot.Digest || a.InputDigest != snapshot.InputDigest || !reviewText(a.Reviewer, 128) || !reviewText(a.Reason, 1024) {
		return nil, ErrInvalid
	}
	stamp, err := time.Parse(time.RFC3339Nano, a.ApprovedAt)
	if err != nil || stamp.Location() != time.UTC {
		return nil, ErrInvalid
	}
	return &Record{Schema: 1, Snapshot: snapshot, Approval: &a}, nil
}
func (s Store) approved(name string) (*Record, string, error) {
	path, err := s.path(name, true)
	if err != nil {
		return nil, "", err
	}
	data, err := read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	record, err := decodeRecord(data, name)
	sum := sha256.Sum256(data)
	return record, hex.EncodeToString(sum[:]), err
}

// Approve rechecks the exact preview under the surface lock before publishing.
// Callers must obtain explicit review; this internal API is not an auth boundary.
func Approve(ctx context.Context, root, name string, expected Report, reviewer, reason string) (record *Record, retErr error) {
	if !reviewText(reviewer, 128) || !reviewText(reason, 1024) {
		return nil, ErrInvalid
	}
	lock, err := Acquire(root, name)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, lock.Close()) }()
	report, snapshot, err := inspect(lock.Root, name)
	if err != nil {
		return nil, err
	}
	if expected.SurfaceID != name || expected.NewDigest != report.NewDigest || expected.InputDigest != report.InputDigest || expected.BaselineRevision != report.BaselineRevision {
		return nil, ErrStale
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	record = &Record{Schema: 1, Snapshot: snapshot, Approval: &Approval{ID: hex.EncodeToString(id), OldDigest: report.OldDigest, NewDigest: report.NewDigest, InputDigest: report.InputDigest, Reviewer: reviewer, Reason: reason, ApprovedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if _, err := decodeRecord(data, name); err != nil {
		return nil, err
	}
	path, err := (Store{Root: lock.Root}).path(name, true)
	if err != nil {
		return nil, err
	}
	// Cancellation before the atomic rename leaves the old record intact.
	err = publish(path, data, func(from, to string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return os.Rename(from, to)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	return record, nil
}
