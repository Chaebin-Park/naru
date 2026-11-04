// Package receipt stores the evidence that a check ran against specific inputs.
package receipt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Schema is the receipt file format version.
const Schema = 1

// Result values.
const (
	ResultPass    = "pass"
	ResultFail    = "fail"
	ResultTimeout = "timeout"
	ResultError   = "error" // the command could not be started
)

// Evidence classes label provenance independently of execution results.
const (
	EvidenceLocal = "local"
	EvidenceAgent = "agent-run"
	EvidenceCI    = "ci"
)

// Receipt is the latest run of one check.
type Receipt struct {
	Schema      int       `json:"schema"`
	Check       string    `json:"check"`
	Command     string    `json:"command"`
	InputsHash  string    `json:"inputs_hash"`
	FileCount   int       `json:"file_count"`
	FileSource  string    `json:"file_source"`
	GitHead     string    `json:"git_head,omitempty"`
	Result      string    `json:"result"`
	ExitCode    int       `json:"exit_code"`
	Evidence    string    `json:"evidence"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationMS  int64     `json:"duration_ms"`
	LogPath     string    `json:"log_path"`
	NaruVersion string    `json:"naru_version"`
}

// Store reads and writes receipts under <root>/.naru/receipts.
type Store struct{ dir string }

// NewStore returns the receipt store for a repository root.
func NewStore(root string) Store {
	return Store{dir: filepath.Join(root, ".naru", "receipts")}
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (s Store) path(check string) (string, error) {
	if !safeName.MatchString(check) {
		return "", fmt.Errorf("check name %q must match [A-Za-z0-9._-]+", check)
	}
	return filepath.Join(s.dir, check+".json"), nil
}

// Get returns the latest receipt, or (nil, nil) when none exists.
func (s Store) Get(check string) (*Receipt, error) {
	p, err := s.path(check)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p) // #nosec G304 -- Check names are validated by Store.path; reads local evidence, not a remote path.
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("receipt %s is corrupt: %w", p, err)
	}
	return &r, nil
}

// Put atomically replaces the latest receipt for r.Check.
func (s Store) Put(r Receipt) (retErr error) {
	p, err := s.path(r.Check)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, r.Check+".*.tmp")
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			retErr = errors.Join(retErr, tmp.Close())
		}
		if err := os.Remove(tmp.Name()); !errors.Is(err, os.ErrNotExist) {
			retErr = errors.Join(retErr, err)
		}
	}()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	closed = true
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
