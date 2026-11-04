package receipt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Chaebin-Park/naru/internal/config"
)

// StateStore keeps schema 2 current state separate from read-only legacy receipts.
// The caller must hold the check's execution lock across Begin and Put.
type StateStore struct {
	root       string
	createTemp func(string, string) (stateFile, error)
	rename     func(string, string) error
	syncDir    func(string) error
}
type stateFile interface {
	io.WriteCloser
	Sync() error
	Name() string
}

func NewStateStore(root string) StateStore { return StateStore{root: root} }

// Evidence contains either the authoritative current record or a legacy receipt.
type Evidence struct {
	Record *Record
	Legacy *Receipt
}

func (s StateStore) path(check string) (string, error) {
	if !config.NamePattern.MatchString(check) {
		return "", fmt.Errorf("%w: check name", ErrInvalid)
	}
	return filepath.Join(s.root, ".naru", "state", "checks", check+".json"), nil
}

// plainPath refuses symlinks and special entries in the local evidence path.
// It does not provide confinement against concurrent hostile filesystem mutation.
func plainPath(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("%w: evidence path", ErrInvalid)
	}
	for p := path; p != root; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("%w: non-regular evidence path", ErrInvalid)
		}
	}
	return nil
}
func (s StateStore) read(path string) ([]byte, error) {
	if err := plainPath(s.root, path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: expected evidence file", ErrInvalid)
	}
	return os.ReadFile(path) // #nosec G304 -- Internal validated evidence path; special entries are rejected above.
}

// Current reads only schema 2 authority; absence is distinct from corruption.
func (s StateStore) Current(check string) (*Record, error) {
	path, err := s.path(check)
	if err != nil {
		return nil, err
	}
	data, err := s.read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return DecodeRecord(data, check)
}

func (s StateStore) Read(check string) (Evidence, error) {
	path, err := s.path(check)
	if err != nil {
		return Evidence{}, err
	}
	data, err := s.read(path)
	if err == nil {
		r, err := DecodeRecord(data, check)
		return Evidence{Record: r}, err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Evidence{}, err
	}
	// Only absence permits legacy lookup, never malformed state or a broken link.
	data, err = s.read(filepath.Join(s.root, ".naru", "receipts", check+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return Evidence{}, nil
	}
	if err != nil {
		return Evidence{}, err
	}
	r, err := DecodeLegacy(data, check)
	return Evidence{Legacy: r}, err
}

// Begin archives an existing current record before installing the new running state.
func (s StateStore) Begin(r Record) error {
	if r.Phase != PhaseRunning {
		return ErrInvalid
	}
	path, err := s.path(r.Check)
	if err != nil {
		return err
	}
	if err := r.Validate(r.Check); err != nil {
		return err
	}
	data, err := s.read(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		old, err := DecodeRecord(data, r.Check)
		if err != nil {
			return err
		}
		if old.RunID == r.RunID {
			return fmt.Errorf("%w: reused run ID", ErrInvalid)
		}
		archive := filepath.Join(s.root, ".naru", "state", "history", r.Check, old.RunID+".json")
		if err := s.archive(archive, data); err != nil {
			return fmt.Errorf("archive previous execution: %w", err)
		}
	}
	return s.Put(r)
}

// Put atomically publishes a complete validated state file.
func (s StateStore) Put(r Record) error {
	if err := r.Validate(r.Check); err != nil {
		return err
	}
	path, err := s.path(r.Check)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return s.atomic(path, append(data, '\n'))
}

func (s StateStore) prepare(path string) error {
	if err := plainPath(s.root, path); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(path), 0o700)
}
func (s StateStore) archive(path string, data []byte) error {
	if err := s.prepare(path); err != nil {
		return err
	}
	existing, err := s.read(path)
	if err == nil {
		if !bytes.Equal(existing, data) {
			return errors.New("history ID already has different content")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Same-check locking serializes this existence check and immutable publication.
	return s.atomic(path, data)
}
func (s StateStore) atomic(path string, data []byte) (retErr error) {
	if err := s.prepare(path); err != nil {
		return err
	}
	create := s.createTemp
	if create == nil {
		create = func(dir, pattern string) (stateFile, error) { return os.CreateTemp(dir, pattern) }
	}
	f, err := create(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	closed := false
	published := false
	defer func() {
		if !closed {
			retErr = errors.Join(retErr, f.Close())
		}
		if !published {
			if err := os.Remove(f.Name()); !errors.Is(err, os.ErrNotExist) {
				retErr = errors.Join(retErr, err)
			}
		}
	}()
	if n, err := f.Write(data); err != nil {
		return err
	} else if n != len(data) {
		return io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		return err
	}
	closed = true
	if err := f.Close(); err != nil {
		return err
	}
	// Do every fallible durability operation before the publication point.
	// The final rename is atomic for process recovery; its directory entry is
	// not claimed durable across power failure.
	syncDir := s.syncDir
	if syncDir == nil {
		syncDir = syncDirectory
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return err
	}
	rename := s.rename
	if rename == nil {
		rename = os.Rename
	}
	if err := rename(f.Name(), path); err != nil {
		return err
	}
	published = true
	return nil
}
