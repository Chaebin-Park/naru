// Package baseline stores extraction candidates separately from read-only baselines.
package baseline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/runlock"
	"github.com/Chaebin-Park/naru/internal/surface"
)

var ErrStorage = errors.New("candidate storage failed")

var ErrInvalid = errors.New("invalid baseline state")
var ErrNotReady = errors.New("candidate is not ready; rerun baseline extract")
var ErrStale = errors.New("candidate inputs changed; rerun baseline extract")

// State replaces previous candidate authority before launching an extractor.
type State struct {
	Schema    int               `json:"schema"`
	SurfaceID string            `json:"surface_id"`
	Phase     string            `json:"phase"`
	Reason    string            `json:"reason"`
	Snapshot  *surface.Snapshot `json:"snapshot"`
}

type Store struct{ Root string }

func Acquire(root, name string) (*runlock.Lock, error) { return runlock.Acquire(root, "surface-"+name) }

func (s Store) path(name string, approved bool) (string, error) {
	if !config.NamePattern.MatchString(name) || len(name) > 128 {
		return "", ErrInvalid
	}
	dir := filepath.Join(s.Root, ".naru", "state", "surfaces")
	if approved {
		dir = filepath.Join(s.Root, ".naru", "baselines")
	}
	path := filepath.Join(dir, name+".json")
	for p := path; p != s.Root; p = filepath.Dir(p) {
		if p == filepath.Dir(p) {
			return "", ErrInvalid
		}
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return "", ErrInvalid
		}
	}
	return path, nil
}

func read(path string) (data []byte, retErr error) {
	f, err := os.Open(path) // #nosec G304 -- Internal evidence path validated by Store.path; caller owns project root.
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	data, err = io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 8<<20 {
		return nil, ErrInvalid
	}
	return data, nil
}

// decodeState rejects duplicate envelope fields before decoding its snapshot.
func decodeState(data []byte, name string) (*State, error) {
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
		if !ok {
			return nil, ErrInvalid
		}
		if _, exists := fields[key]; exists {
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
	if len(fields) != 5 {
		return nil, ErrInvalid
	}
	for _, key := range []string{"schema", "surface_id", "phase", "reason", "snapshot"} {
		raw, ok := fields[key]
		if !ok || (key != "snapshot" && bytes.Equal(raw, []byte("null"))) {
			return nil, ErrInvalid
		}
	}
	var w struct {
		Schema    int             `json:"schema"`
		SurfaceID string          `json:"surface_id"`
		Phase     string          `json:"phase"`
		Reason    string          `json:"reason"`
		Snapshot  json.RawMessage `json:"snapshot"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, ErrInvalid
	}
	s := &State{Schema: w.Schema, SurfaceID: w.SurfaceID, Phase: w.Phase, Reason: w.Reason}
	if s.Schema != 1 || s.SurfaceID != name || len(s.Reason) > 512 {
		return nil, ErrInvalid
	}
	if s.Phase == "ready" {
		if s.Reason != "" {
			return nil, ErrInvalid
		}
		snapshot, err := surface.DecodeSnapshot(w.Snapshot, name)
		if err != nil {
			return nil, err
		}
		s.Snapshot = snapshot
	} else {
		if s.Phase != "running" && s.Phase != "failed" && s.Phase != "recovery" {
			return nil, ErrInvalid
		}
		if !bytes.Equal(w.Snapshot, []byte("null")) || (s.Phase == "running" && s.Reason != "") || (s.Phase != "running" && s.Reason == "") {
			return nil, ErrInvalid
		}
	}
	return s, nil
}

func (s Store) Current(name string) (*State, error) {
	path, err := s.path(name, false)
	if err != nil {
		return nil, err
	}
	data, err := read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeState(data, name)
}

// Put publishes a complete state under the caller's surface lock. A failed
// write leaves the prior state; a prior running state can never satisfy diff.
func (s Store) Put(state State) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = fmt.Errorf("%w: %w", ErrStorage, retErr)
		}
	}()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if _, err := decodeState(data, state.SurfaceID); err != nil {
		return err
	}
	path, err := s.path(state.SurfaceID, false)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

func atomicWrite(path string, data []byte) error {
	return publish(path, data, os.Rename)
}

func publish(path string, data []byte, rename func(string, string) error) (retErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".candidate-*.tmp")
	if err != nil {
		return err
	}
	closed, published := false, false
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
	if err := rename(f.Name(), path); err != nil {
		return fmt.Errorf("publish candidate: %w", err)
	}
	published = true
	return nil
}
