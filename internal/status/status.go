// Package status decides whether the required checks are satisfied for the current inputs.
package status

import (
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/receipt"
)

// State of one check relative to the current inputs.
type State string

const (
	Pass    State = "pass"    // latest receipt passed and matches current inputs
	Fail    State = "fail"    // latest receipt matches current inputs but did not pass
	Stale   State = "stale"   // inputs or command changed since the latest receipt
	Missing State = "missing" // never run (not-run is never a pass)
	Empty   State = "empty"   // inputs match no files, so a receipt would prove nothing
)

// CheckStatus is one row of a status report.
type CheckStatus struct {
	RunID       *string             `json:"run_id"`
	Reason      string              `json:"reason"`
	Message     string              `json:"message"`
	Provenance  *receipt.Provenance `json:"provenance"`
	Record      *receipt.Record     `json:"record,omitempty"`
	Check       string              `json:"check"`
	State       State               `json:"state"`
	CurrentHash string              `json:"current_hash"`
	Receipt     *receipt.Receipt    `json:"receipt,omitempty"`
	Relay       *RelayStatus        `json:"relay,omitempty"`
}

// RelayStatus exposes provenance and validity times, not submitted artifacts.
type RelayStatus struct {
	PacketID   string     `json:"packet_id"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
}

// Report is the status of every check a mode requires.
type Report struct {
	Surfaces   []SurfaceStatus `json:"surfaces,omitempty"`
	Schema     int             `json:"schema"`
	Unverified UnverifiedState `json:"unverified"`
	Mode       string          `json:"mode"`
	Satisfied  bool            `json:"satisfied"`
	Checks     []CheckStatus   `json:"checks"`
}

// Evaluate computes the report for mode without running anything.
func Evaluate(root string, cfg *config.Config, mode string) (Report, error) {
	return EvaluateState(root, cfg, mode)
}

// SurfaceStatus is a required contract baseline evaluated against current inputs.
type SurfaceStatus struct {
	Surface          string `json:"surface"`
	State            string `json:"state"`
	InputDigest      string `json:"input_digest"`
	CandidateDigest  string `json:"candidate_digest"`
	BaselineRevision string `json:"baseline_revision"`
}
