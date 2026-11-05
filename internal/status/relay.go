package status

import (
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/receipt"
)

// evaluateRelay runs under the check lock. Remote execution does not require
// local tools, but its submitted evidence must still match current local inputs.
func evaluateRelay(root string, cfg *config.Config, name string, r receipt.RelayRecord, now time.Time) (CheckStatus, error) {
	row := CheckStatus{Check: name, RunID: &r.RunID, Provenance: &r.Provenance,
		Relay: &RelayStatus{PacketID: r.Packet.PacketID, ExpiresAt: r.Packet.ExpiresAt}}
	set := func(state State, reason, message string) (CheckStatus, error) {
		row.State, row.Reason, row.Message = state, reason, message
		return row, nil
	}
	if err := r.Validate(name); err != nil {
		return set(Invalid, "invalid_receipt", "relay receipt is corrupt or inconsistent")
	}
	if r.Acceptance != nil {
		row.Relay.AcceptedAt = &r.Acceptance.AcceptedAt
	}
	if !now.Before(r.Packet.ExpiresAt) {
		return set(Stale, "relay_expired", "relay request expired; issue a new request or run the check locally")
	}
	if now.Before(r.Packet.IssuedAt) || (r.Acceptance != nil && now.Before(r.Acceptance.AcceptedAt)) {
		return set(Invalid, "relay_clock_invalid", "current time precedes relay evidence; verification cannot be established")
	}
	if r.Phase == receipt.PhasePending {
		return set(Missing, "relay_pending", "relay request is pending; submit its result or run the check locally")
	}
	if err := receipt.ValidateRelayEvidence(root, r, now); err != nil {
		return set(Invalid, "relay_artifact_invalid", "relay artifact evidence is missing, changed or unreadable")
	}
	fingerprint := cfg.Fingerprint(name)
	if !receipt.RelayDefinitionMatches(r.Packet, cfg) {
		return set(Stale, "inputs_changed", "check definition changed since the relay request")
	}
	ch := cfg.Checks[name]
	d, err := fileset.Compute(root, ch.Inputs, cfg.Exclude, fingerprint)
	if err != nil {
		return row, err
	}
	row.CurrentHash = d.Hash
	if d.FileCount == 0 {
		return set(Empty, "empty_inputs", "inputs match no files")
	}
	if r.Packet.InputDigest.Hash != d.Hash || r.Packet.InputDigest.FileCount != d.FileCount || r.Packet.InputDigest.FileSource != string(d.Source) {
		return set(Stale, "inputs_changed", "inputs or check definition changed since the relay request")
	}
	if r.Acceptance.Result != receipt.ResultPass {
		return set(Fail, "relay_failed", "human-relayed execution did not pass")
	}
	return set(Pass, "relay_verified", "human-relayed check passed and current inputs and artifacts match")
}
