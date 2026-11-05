package receipt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Chaebin-Park/naru/internal/config"
)

const RelaySchema = 3
const RelayInputPolicy = "human-relay-v1"
const PhasePending = "pending"
const EvidenceHumanRelayed = "human-relayed"
const RelayJSONLimit = 1 << 20
const RelayArtifactLimit = 16 << 20
const RelayTotalLimit = 64 << 20
const RelayArtifactCount = 16

type RelayPacket struct {
	Schema            int               `json:"schema"`
	PacketID          string            `json:"packet_id"`
	Check             string            `json:"check"`
	IssuedAt          time.Time         `json:"issued_at"`
	ExpiresAt         time.Time         `json:"expires_at"`
	InputDigest       InputDigest       `json:"input_digest"`
	DefinitionHash    string            `json:"definition_hash"`
	Command           string            `json:"command"`
	Workdir           string            `json:"workdir"`
	TimeoutMS         int64             `json:"timeout_ms"`
	Inputs            []string          `json:"inputs"`
	Excludes          []string          `json:"excludes"`
	Requirements      RelayRequirements `json:"requirements"`
	Target            string            `json:"target"`
	ExpectedArtifacts []string          `json:"expected_artifacts"`
}
type RelayRequirements struct {
	Tools []string `json:"tools"`
	OS    []string `json:"os"`
}
type RelayToolVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type RelayArtifactInput struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type RelaySubmission struct {
	Schema            int                  `json:"schema"`
	PacketID          string               `json:"packet_id"`
	PacketHash        string               `json:"packet_hash"`
	Check             string               `json:"check"`
	SubmittedBy       string               `json:"submitted_by"`
	StartedAt         time.Time            `json:"started_at"`
	FinishedAt        time.Time            `json:"finished_at"`
	BeforeInputDigest InputDigest          `json:"before_input_digest"`
	AfterInputDigest  InputDigest          `json:"after_input_digest"`
	Result            string               `json:"result"`
	ExitCode          *int                 `json:"exit_code"`
	ToolVersions      []RelayToolVersion   `json:"tool_versions"`
	Artifacts         []RelayArtifactInput `json:"artifacts"`
}
type RelayArtifact struct {
	Name       string `json:"name"`
	SHA256     string `json:"sha256"`
	StoredPath string `json:"stored_path"`
	Size       int64  `json:"size"`
}
type RelayAcceptance struct {
	AcceptedAt        time.Time          `json:"accepted_at"`
	SubmittedBy       string             `json:"submitted_by"`
	StartedAt         time.Time          `json:"started_at"`
	FinishedAt        time.Time          `json:"finished_at"`
	BeforeInputDigest InputDigest        `json:"before_input_digest"`
	AfterInputDigest  InputDigest        `json:"after_input_digest"`
	Result            string             `json:"result"`
	ExitCode          *int               `json:"exit_code"`
	ToolVersions      []RelayToolVersion `json:"tool_versions"`
	Artifacts         []RelayArtifact    `json:"artifacts"`
}
type RelayRecord struct {
	Schema      int              `json:"schema"`
	InputPolicy string           `json:"input_policy"`
	Check       string           `json:"check"`
	RunID       string           `json:"run_id"`
	Phase       string           `json:"phase"`
	Provenance  Provenance       `json:"provenance"`
	Packet      RelayPacket      `json:"packet"`
	PacketHash  string           `json:"packet_hash"`
	Acceptance  *RelayAcceptance `json:"acceptance,omitempty"`
}

var relayArtifactName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func relayText(s string) bool {
	if strings.TrimSpace(s) == "" || len(s) > 1024 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func relayCommand(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}
func relayNames(names []string, minimum int) bool {
	if names == nil || len(names) < minimum || len(names) > RelayArtifactCount {
		return false
	}
	seen := map[string]bool{}
	for _, name := range names {
		if !relayArtifactName.MatchString(name) || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}
func (p RelayPacket) Validate() error {
	if p.Schema != 1 {
		return ErrUnsupported
	}
	if !runIDPattern.MatchString(p.PacketID) || !config.NamePattern.MatchString(p.Check) || !validDigest(p.InputDigest) || !hashPattern.MatchString(p.DefinitionHash) {
		return fmt.Errorf("%w: packet identity", ErrInvalid)
	}
	if !utc(p.IssuedAt) || !utc(p.ExpiresAt) || !p.ExpiresAt.After(p.IssuedAt) || p.ExpiresAt.Sub(p.IssuedAt) > 7*24*time.Hour {
		return fmt.Errorf("%w: packet expiry", ErrInvalid)
	}
	if !relayText(p.Target) || !relayCommand(p.Command) || p.TimeoutMS <= 0 || !relayText(p.Workdir) || !filepath.IsLocal(p.Workdir) || p.Inputs == nil || len(p.Inputs) == 0 || p.Excludes == nil || p.Requirements.Tools == nil || p.Requirements.OS == nil || !relayNames(p.ExpectedArtifacts, 1) {
		return fmt.Errorf("%w: packet definition", ErrInvalid)
	}
	for _, list := range [][]string{p.Inputs, p.Excludes, p.Requirements.Tools, p.Requirements.OS} {
		for _, s := range list {
			if !relayText(s) {
				return fmt.Errorf("%w: packet text", ErrInvalid)
			}
		}
	}
	return nil
}
func relayResult(result string, code *int) bool {
	switch result {
	case ResultPass:
		return code != nil && *code == 0
	case ResultFail:
		return code != nil && *code != 0 && *code >= -1
	case ResultTimeout, ResultError:
		return code == nil || *code >= -1
	}
	return false
}
func relayVersions(v []RelayToolVersion) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	seen := map[string]bool{}
	for _, x := range v {
		if !relayText(x.Name) || !relayText(x.Version) || seen[x.Name] {
			return false
		}
		seen[x.Name] = true
	}
	return true
}
func (s RelaySubmission) Validate() error {
	if s.Schema != 1 {
		return ErrUnsupported
	}
	if !runIDPattern.MatchString(s.PacketID) || !hashPattern.MatchString(s.PacketHash) || !config.NamePattern.MatchString(s.Check) || !relayText(s.SubmittedBy) || !utc(s.StartedAt) || !utc(s.FinishedAt) || s.FinishedAt.Before(s.StartedAt) || !validDigest(s.BeforeInputDigest) || !validDigest(s.AfterInputDigest) || !relayResult(s.Result, s.ExitCode) || !relayVersions(s.ToolVersions) {
		return fmt.Errorf("%w: submission metadata", ErrInvalid)
	}
	names := make([]string, 0, len(s.Artifacts))
	for _, a := range s.Artifacts {
		names = append(names, a.Name)
		if !relayText(a.Path) || !hashPattern.MatchString(a.SHA256) {
			return fmt.Errorf("%w: artifact metadata", ErrInvalid)
		}
	}
	if !relayNames(names, 1) {
		return fmt.Errorf("%w: artifact names", ErrInvalid)
	}
	return nil
}
func PacketHash(p RelayPacket) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}
func (r RelayRecord) Validate(check string) error {
	if r.Schema != RelaySchema || r.InputPolicy != RelayInputPolicy {
		return ErrUnsupported
	}
	if err := r.Packet.Validate(); err != nil {
		return err
	}
	hash, err := PacketHash(r.Packet)
	if err != nil {
		return err
	}
	if r.Check != check || r.Packet.Check != check || r.RunID != r.Packet.PacketID || r.Provenance.Kind != EvidenceHumanRelayed || hash != r.PacketHash {
		return fmt.Errorf("%w: relay identity", ErrInvalid)
	}
	if r.Phase == PhasePending && r.Acceptance == nil {
		return nil
	}
	a := r.Acceptance
	if r.Phase != PhaseTerminal || a == nil {
		return fmt.Errorf("%w: relay phase", ErrInvalid)
	}
	if !utc(a.AcceptedAt) || a.AcceptedAt.Before(a.FinishedAt) || !a.AcceptedAt.Before(r.Packet.ExpiresAt) || a.StartedAt.Before(r.Packet.IssuedAt) || a.BeforeInputDigest != r.Packet.InputDigest || a.AfterInputDigest != r.Packet.InputDigest {
		return fmt.Errorf("%w: relay acceptance", ErrInvalid)
	}
	s := RelaySubmission{Schema: 1, PacketID: r.RunID, PacketHash: r.PacketHash, Check: check, SubmittedBy: a.SubmittedBy, StartedAt: a.StartedAt, FinishedAt: a.FinishedAt, BeforeInputDigest: a.BeforeInputDigest, AfterInputDigest: a.AfterInputDigest, Result: a.Result, ExitCode: a.ExitCode, ToolVersions: a.ToolVersions}
	expected := map[string]bool{}
	for _, name := range r.Packet.ExpectedArtifacts {
		expected[name] = true
	}
	var total int64
	attempt := ""
	for _, x := range a.Artifacts {
		s.Artifacts = append(s.Artifacts, RelayArtifactInput{Name: x.Name, Path: x.StoredPath, SHA256: x.SHA256})
		parts := strings.Split(x.StoredPath, "/")
		if len(parts) != 6 || parts[0] != ".naru" || parts[1] != "relay" || parts[2] != "artifacts" || parts[3] != r.RunID || !runIDPattern.MatchString(parts[4]) || parts[5] != x.Name || !expected[x.Name] || x.Size < 0 || x.Size > RelayArtifactLimit {
			return fmt.Errorf("%w: stored artifact", ErrInvalid)
		}
		if attempt != "" && attempt != parts[4] {
			return fmt.Errorf("%w: mixed artifact attempts", ErrInvalid)
		}
		attempt = parts[4]
		delete(expected, x.Name)
		total += x.Size
	}
	if len(expected) != 0 || total > RelayTotalLimit {
		return fmt.Errorf("%w: required artifacts", ErrInvalid)
	}
	return s.Validate()
}

// relayDecode extends local strict decoding through arrays and optional objects.
func relayDecode(data []byte, dst any) error {
	if len(data) > RelayJSONLimit || !utf8.Valid(data) {
		return fmt.Errorf("%w: JSON size or encoding", ErrInvalid)
	}
	if err := strictDecode(data, dst); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := relayShape(data, reflect.TypeOf(dst).Elem()); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}
func relayShape(data []byte, typ reflect.Type) error {
	if strings.TrimSpace(string(data)) == "null" && typ != reflect.TypeOf((*int)(nil)) {
		return fmt.Errorf("unexpected null")
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return nil
	}
	if typ.Kind() == reflect.Slice {
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for _, v := range values {
			if err := relayShape(v, typ.Elem()); err != nil {
				return err
			}
		}
	}
	if typ.Kind() == reflect.Struct {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for n := 0; n < typ.NumField(); n++ {
			f := typ.Field(n)
			tag := strings.Split(f.Tag.Get("json"), ",")
			raw, ok := values[tag[0]]
			if !ok {
				if len(tag) > 1 && tag[1] == "omitempty" {
					continue
				}
				return fmt.Errorf("missing %s", tag[0])
			}
			if err := relayShape(raw, f.Type); err != nil {
				return err
			}
			delete(values, tag[0])
		}
		if len(values) != 0 {
			return fmt.Errorf("unknown or mis-cased field")
		}
	}
	return nil
}
func DecodeRelayPacket(data []byte) (*RelayPacket, error) {
	var p RelayPacket
	if err := relayDecode(data, &p); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}
func DecodeRelaySubmission(data []byte) (*RelaySubmission, error) {
	var s RelaySubmission
	if err := relayDecode(data, &s); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}
func DecodeRelayRecord(data []byte, check string) (*RelayRecord, error) {
	var r RelayRecord
	if err := relayDecode(data, &r); err != nil {
		return nil, err
	}
	if err := r.Validate(check); err != nil {
		return nil, err
	}
	return &r, nil
}
