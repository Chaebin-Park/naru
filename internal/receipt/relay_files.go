package receipt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
)

// RelaySafePath rejects symlinks and special entries beneath the selected root.
// Like other evidence storage, it does not defend against hostile filesystem races.
func RelaySafePath(root, path string) error { return plainPath(root, path) }

// ReadRelayFile bounds regular-file reads and rejects unsafe path components.
func ReadRelayFile(root, path string, limit int64) (data []byte, retErr error) {
	if err := plainPath(root, path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("%w: file type or size", ErrInvalid)
	}
	f, err := os.Open(path) // #nosec G304 -- Caller-selected root, confined path, regular-file and size checks above.
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); retErr == nil {
			retErr = err
		}
	}()
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: file size", ErrInvalid)
	}
	return data, nil
}

// ValidateRelayEvidence checks immutable copies, never the human's source paths.
// Result and current input/definition comparison belong to the evaluating caller.
func ValidateRelayEvidence(root string, r RelayRecord, now time.Time) error {
	if err := r.Validate(r.Check); err != nil {
		return err
	}
	if r.Phase != PhaseTerminal || r.Acceptance == nil {
		return fmt.Errorf("%w: pending relay", ErrInvalid)
	}
	if now.Before(r.Acceptance.AcceptedAt) || !now.Before(r.Packet.ExpiresAt) {
		return fmt.Errorf("%w: relay clock or expiry", ErrInvalid)
	}
	for _, a := range r.Acceptance.Artifacts {
		data, err := ReadRelayFile(root, filepath.Join(root, filepath.FromSlash(a.StoredPath)), RelayArtifactLimit)
		if err != nil {
			return err
		}
		if int64(len(data)) != a.Size || fmt.Sprintf("sha256:%x", sha256.Sum256(data)) != a.SHA256 {
			return fmt.Errorf("%w: artifact changed", ErrInvalid)
		}
	}
	return nil
}

// RelayDefinitionMatches also compares timeout, which the shared fingerprint
// does not include, and preserves the exact request's configured selections.
func RelayDefinitionMatches(p RelayPacket, cfg *config.Config) bool {
	ch, ok := cfg.Checks[p.Check]
	if !ok {
		return false
	}
	workdir := ch.Workdir
	if workdir == "" {
		workdir = "."
	}
	empty := func(s []string) []string { return append([]string{}, s...) }
	return p.DefinitionHash == fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(cfg.Fingerprint(p.Check)))) && p.Command == ch.Run && p.Workdir == workdir && p.TimeoutMS == ch.TimeoutDuration().Milliseconds() && reflect.DeepEqual(p.Inputs, empty(ch.Inputs)) && reflect.DeepEqual(p.Excludes, empty(cfg.Exclude)) && reflect.DeepEqual(p.Requirements.Tools, empty(ch.Requires.Tools)) && reflect.DeepEqual(p.Requirements.OS, empty(ch.Requires.OS))
}

// ExportRelayPacket publishes an immutable request before pending authority.
func (s StateStore) ExportRelayPacket(p RelayPacket) error {
	if err := p.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > RelayJSONLimit {
		return fmt.Errorf("%w: packet too large", ErrInvalid)
	}
	path := filepath.Join(s.root, ".naru", "relay", "packets", p.PacketID+".json")
	return s.archive(path, append(data, '\n'))
}

// SyncRelayDirectory flushes publication entries where directory sync is supported.
func SyncRelayDirectory(path string) error { return syncDirectory(path) }
