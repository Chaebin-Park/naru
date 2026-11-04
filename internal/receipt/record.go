package receipt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/environment"
)

// StateSchema and InputPolicy identify the execution evidence contract.
const StateSchema = 2
const InputPolicy = "before-after-v1"
const (
	PhaseRunning      = "running"
	PhaseTerminal     = "terminal"
	ResultNotRun      = "not-run"
	InputsUnobserved  = "unobserved"
	ResultCancelled   = "cancelled"
	ResultInterrupted = "interrupted"
	InputsPending     = "pending"
	InputsMatched     = "matched"
	InputsChanged     = "changed"
	InputsUnreadable  = "unreadable"
)

// ErrInvalid and ErrUnsupported distinguish unusable evidence from IO failures.
var ErrInvalid = errors.New("invalid receipt")
var ErrUnsupported = errors.New("unsupported receipt contract")

// Record is the single authoritative state of one execution.
type Record struct {
	Schema     int        `json:"schema"`
	Check      string     `json:"check"`
	RunID      string     `json:"run_id"`
	Phase      string     `json:"phase"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Provenance Provenance `json:"provenance"`
	Execution  Execution  `json:"execution"`
	Inputs     Inputs     `json:"inputs"`
}

type Provenance struct {
	Kind string `json:"kind"`
}
type Execution struct {
	Unavailable *environment.Unavailable `json:"unavailable,omitempty"`
	Command     string                   `json:"command"`
	Workdir     string                   `json:"workdir"`
	TimeoutMS   int64                    `json:"timeout_ms"`
	Result      *string                  `json:"result"`
	ExitCode    *int                     `json:"exit_code"`
	DurationMS  *int64                   `json:"duration_ms"`
	LogPath     string                   `json:"log_path"`
	NaruVersion string                   `json:"naru_version"`
}
type InputDigest struct {
	Hash       string `json:"hash"`
	FileCount  int    `json:"file_count"`
	FileSource string `json:"file_source"`
}
type Inputs struct {
	Policy   string       `json:"policy"`
	Before   InputDigest  `json:"before"`
	After    *InputDigest `json:"after"`
	Validity string       `json:"validity"`
	Reason   *string      `json:"reason"`
}

var runIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var hashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validEvidence(s string) bool { return s == EvidenceLocal || s == EvidenceAgent || s == EvidenceCI }
func validDigest(d InputDigest) bool {
	return hashPattern.MatchString(d.Hash) && d.FileCount > 0 && (d.FileSource == "git" || d.FileSource == "walk")
}
func utc(t time.Time) bool { _, offset := t.Zone(); return !t.IsZero() && offset == 0 }

// Validate rejects inconsistent evidence before either storage or evaluation.
func (r Record) Validate(check string) error {
	bad := func(why string) error { return fmt.Errorf("%w: %s", ErrInvalid, why) }
	if r.Schema != StateSchema || r.Inputs.Policy != InputPolicy {
		return ErrUnsupported
	}
	if !config.NamePattern.MatchString(check) || r.Check != check {
		return bad("check identity")
	}
	if !runIDPattern.MatchString(r.RunID) {
		return bad("run identity")
	}
	if !utc(r.StartedAt) || !validEvidence(r.Provenance.Kind) {
		return bad("start or provenance")
	}
	e, i := r.Execution, r.Inputs
	if e.Command == "" || e.Workdir == "" || e.TimeoutMS <= 0 || e.NaruVersion == "" {
		return bad("execution definition")
	}
	if e.LogPath != ".naru/logs/"+check+"-"+r.RunID+".log" {
		return bad("log identity")
	}
	if e.Result != nil && *e.Result == ResultNotRun {
		if r.Phase != PhaseTerminal || r.FinishedAt == nil || !utc(*r.FinishedAt) || r.FinishedAt.Before(r.StartedAt) || e.ExitCode != nil || e.DurationMS == nil || *e.DurationMS != 0 || e.Unavailable == nil || !e.Unavailable.Validate() || i.Validity != InputsUnobserved || i.Before != (InputDigest{}) || i.After != nil || i.Reason != nil {
			return bad("not-run fields")
		}
		return nil
	}
	if e.Unavailable != nil {
		return bad("unavailable requires not-run")
	}
	if !validDigest(i.Before) || (i.After != nil && !validDigest(*i.After)) {
		return bad("input digest")
	}
	if r.Phase == PhaseRunning {
		if r.FinishedAt != nil || e.Result != nil || e.ExitCode != nil || e.DurationMS != nil || i.After != nil || i.Validity != InputsPending || i.Reason != nil {
			return bad("running fields")
		}
		return nil
	}
	if r.Phase != PhaseTerminal || r.FinishedAt == nil || !utc(*r.FinishedAt) || r.FinishedAt.Before(r.StartedAt) || e.Result == nil || e.DurationMS == nil || *e.DurationMS < 0 {
		return bad("terminal fields")
	}
	switch *e.Result {
	case ResultPass:
		if e.ExitCode == nil || *e.ExitCode != 0 {
			return bad("pass exit code")
		}
	case ResultFail:
		if e.ExitCode == nil || *e.ExitCode == 0 || *e.ExitCode < -1 {
			return bad("fail exit code")
		}
	case ResultTimeout, ResultError, ResultCancelled, ResultInterrupted:
		if e.ExitCode != nil && *e.ExitCode < -1 {
			return bad("exit code")
		}
	default:
		return bad("result")
	}
	switch i.Validity {
	case InputsMatched:
		if i.After == nil || i.Before.Hash != i.After.Hash || i.Before.FileCount != i.After.FileCount || i.Reason != nil {
			return bad("matched inputs")
		}
	case InputsChanged:
		if i.After == nil || (i.Before.Hash == i.After.Hash && i.Before.FileCount == i.After.FileCount) || i.Reason == nil || (*i.Reason != "inputs_changed" && *i.Reason != "definition_changed") {
			return bad("changed inputs")
		}
	case InputsUnreadable:
		if i.After != nil || i.Reason == nil {
			return bad("unreadable inputs")
		}
		switch *i.Reason {
		case "input_read_failed", "definition_unreadable", "empty_inputs", "owner_lost":
		default:
			return bad("input reason")
		}
	default:
		return bad("input validity")
	}
	return nil
}

// DecodeRecord requires every non-optional key, including explicit nullable keys.
func DecodeRecord(data []byte, check string) (*Record, error) {
	var r Record
	if err := strictDecode(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := r.Validate(check); err != nil {
		return nil, err
	}
	return &r, nil
}

// DecodeLegacy preserves original fields; no after evidence is inferred.
func DecodeLegacy(data []byte, check string) (*Receipt, error) {
	var r Receipt
	if err := strictDecode(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if r.Schema != Schema {
		return nil, ErrUnsupported
	}
	if r.Check != check || !config.NamePattern.MatchString(check) || r.Command == "" || !validDigest(InputDigest{r.InputsHash, r.FileCount, r.FileSource}) || !validEvidence(r.Evidence) || !utc(r.StartedAt) || !utc(r.FinishedAt) || r.FinishedAt.Before(r.StartedAt) || r.DurationMS < 0 || r.LogPath == "" || r.NaruVersion == "" {
		return nil, ErrInvalid
	}
	switch r.Result {
	case ResultPass:
		if r.ExitCode != 0 {
			return nil, ErrInvalid
		}
	case ResultFail:
		if r.ExitCode == 0 || r.ExitCode < -1 {
			return nil, ErrInvalid
		}
	case ResultTimeout, ResultError:
		if r.ExitCode < -1 {
			return nil, ErrInvalid
		}
	default:
		return nil, ErrInvalid
	}
	return &r, nil
}

func strictDecode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := uniqueValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	return requiredFields(data, reflect.TypeOf(dst).Elem())
}

func uniqueValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return errors.New("duplicate or invalid key")
			}
			seen[s] = true
			if err := uniqueValue(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case json.Delim('['):
		for d.More() {
			if err := uniqueValue(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	return nil
}

func requiredFields(data []byte, typ reflect.Type) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		if typ.Kind() == reflect.Pointer {
			return nil
		}
		return errors.New("unexpected null")
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(time.Time{}) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil || !strings.HasSuffix(s, "Z") {
			return errors.New("timestamp must be UTC")
		}
		return nil
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	for n := 0; n < typ.NumField(); n++ {
		field := typ.Field(n)
		tag := strings.Split(field.Tag.Get("json"), ",")
		raw, ok := obj[tag[0]]
		if !ok {
			if len(tag) > 1 && tag[1] == "omitempty" {
				continue
			}
			return fmt.Errorf("missing %s", tag[0])
		}
		if tag[0] == "finished_at" && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("finished_at must be omitted or a timestamp")
		}
		if err := requiredFields(raw, field.Type); err != nil {
			return fmt.Errorf("%s: %w", tag[0], err)
		}
	}
	return nil
}
