// Package policy indexes tool rules and approves only reductions in known violations.
package policy

import "errors"

var (
	ErrInvalid      = errors.New("invalid policy configuration or data")
	ErrNotReady     = errors.New("policy candidate is not ready; run policy check")
	ErrStale        = errors.New("policy preview changed; run policy check again")
	ErrIncompatible = errors.New("policy definition changed; baseline migration is not supported")
	ErrGrowth       = errors.New("new violations cannot be approved as a reduction")
	ErrStorage      = errors.New("policy storage failed")
)

type Index struct {
	Version          int               `yaml:"version" json:"version"`
	Surface          string            `yaml:"surface" json:"surface"`
	DefinitionFiles  []string          `yaml:"definition_files" json:"definition_files"`
	Sources          map[string]string `yaml:"sources" json:"sources"`
	Rules            map[string]Rule   `yaml:"rules" json:"rules"`
	DefinitionDigest string            `yaml:"-" json:"definition_digest"`
}
type Rule struct {
	Tool       string    `yaml:"tool" json:"tool"`
	ToolConfig string    `yaml:"tool_config" json:"tool_config"`
	Source     string    `yaml:"source" json:"source"`
	Fixtures   []Fixture `yaml:"fixtures" json:"fixtures"`
}
type Fixture struct {
	Project string `yaml:"project" json:"project"`
	Expect  string `yaml:"expect" json:"expect"`
}
type Finding struct {
	ID             string `json:"id"`
	RuleID         string `json:"rule_id"`
	StableLocation string `json:"stable_location"`
	Fingerprint    string `json:"fingerprint"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	Message        string `json:"message"`
}
type Report struct {
	State            string    `json:"state"`
	DefinitionDigest string    `json:"definition_digest"`
	CandidateDigest  string    `json:"candidate_digest"`
	InputDigest      string    `json:"input_digest"`
	BaselineRevision string    `json:"baseline_revision"`
	Known            []Finding `json:"known"`
	New              []Finding `json:"new"`
	Resolved         []Finding `json:"resolved"`
}
type Review struct {
	Kind   string `json:"kind"`
	Report Report `json:"report"`
}
type Record struct {
	Schema           int       `json:"schema"`
	DefinitionDigest string    `json:"definition_digest"`
	Findings         []Finding `json:"findings"`
	Approval         Approval  `json:"approval"`
}
type Approval struct {
	Kind             string  `json:"kind"`
	PreviousRevision *string `json:"previous_revision"`
	CandidateDigest  string  `json:"candidate_digest"`
	InputDigest      string  `json:"input_digest"`
	Reviewer         string  `json:"reviewer"`
	Reason           string  `json:"reason"`
	ApprovedAt       string  `json:"approved_at"`
}
type FixtureReport struct {
	Results []FixtureResult `json:"results"`
	Passed  bool            `json:"passed"`
}
type FixtureResult struct {
	RuleID  string `json:"rule_id"`
	Project string `json:"project"`
	Expect  string `json:"expect"`
	Error   string `json:"error"`
	Passed  bool   `json:"passed"`
}
