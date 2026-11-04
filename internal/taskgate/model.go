// Package taskgate enforces local task transitions against committed evidence.
package taskgate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const Version = 1
const Profile = "code-v1"
const ledgerPath = "docs/tasks/workflow.json"

var taskID = regexp.MustCompile(`^NARU-[0-9]{3}(\.[0-9]+)?$`)
var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Criterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Test        string `json:"test"`
}
type Plan struct {
	Title       string      `json:"title"`
	Profile     string      `json:"profile"`
	Criteria    []Criterion `json:"criteria"`
	Limitations []string    `json:"limitations"`
}
type Task struct {
	Schema         int    `json:"schema"`
	ID             string `json:"id"`
	Previous       string `json:"previous"`
	Base           string `json:"base"`
	Status         string `json:"status"`
	Plan           Plan   `json:"plan"`
	Implementation string `json:"implementation"`
	Evidence       string `json:"evidence"`
}
type Workflow struct {
	Schema       int    `json:"schema"`
	Branch       string `json:"branch"`
	Active       string `json:"active"`
	Completed    string `json:"completed"`
	ImportedBase string `json:"imported_base"`
}
type Result struct {
	Schema     int      `json:"schema"`
	Action     string   `json:"action"`
	Task       string   `json:"task"`
	Allowed    bool     `json:"allowed"`
	Head       string   `json:"head"`
	Previous   string   `json:"previous_task"`
	Reasons    []string `json:"reasons"`
	NextAction string   `json:"next_action"`
}
type failure struct {
	reason string
	code   int
	detail string
}

func (e *failure) Error() string       { return e.reason + ": " + e.detail }
func deny(reason, detail string) error { return &failure{reason, 1, detail} }
func invalid(detail string) error      { return &failure{"INVALID_DATA", 70, detail} }
func taskPath(id string) string        { return "docs/tasks/" + id + ".json" }
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// decode rejects duplicates, unknown keys, nulls and missing fields, recursively.
func decode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var value func() error
	value = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		if t == nil {
			return errors.New("null is not allowed")
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("duplicate or invalid key")
				}
				seen[key] = true
				if e = value(); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e := value(); e != nil {
					return e
				}
			}
		default:
			return errors.New("unexpected delimiter")
		}
		_, e = d.Token()
		return e
	}
	if err := value(); err != nil {
		return invalid(err.Error())
	}
	if _, err := d.Token(); err != io.EOF {
		return invalid("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return invalid(err.Error())
	}
	// All fields in this contract are required. Compare key structure to the typed encoding.
	encoded, err := json.Marshal(dst)
	if err != nil {
		return err
	}
	var actual, expected any
	if err = json.Unmarshal(data, &actual); err != nil {
		return err
	}
	if err = json.Unmarshal(encoded, &expected); err != nil {
		return err
	}
	var keys func(any, any) bool
	keys = func(a, b any) bool {
		switch b := b.(type) {
		case map[string]any:
			m, ok := a.(map[string]any)
			if !ok || len(m) != len(b) {
				return false
			}
			for k, v := range b {
				av, ok := m[k]
				if !ok || !keys(av, v) {
					return false
				}
			}
		case []any:
			m, ok := a.([]any)
			if !ok || len(m) != len(b) {
				return false
			}
			for i := range b {
				if !keys(m[i], b[i]) {
					return false
				}
			}
		}
		return true
	}
	if !keys(actual, expected) {
		return invalid("missing required fields")
	}
	return nil
}
func (p Plan) validate() error {
	if p.Title == "" || p.Profile != Profile || len(p.Criteria) == 0 {
		return invalid("title, code-v1 profile and acceptance tests are required")
	}
	seen := map[string]bool{}
	for _, c := range p.Criteria {
		if c.ID == "" || seen[c.ID] || c.Description == "" || !regexp.MustCompile(`^[A-Za-z0-9._/-]+/Test[A-Za-z0-9_/]+$`).MatchString(c.Test) {
			return invalid("acceptance criteria require unique IDs and package/TestName evidence")
		}
		seen[c.ID] = true
	}
	return nil
}
func (t Task) validate() error {
	if t.Schema != Version || !taskID.MatchString(t.ID) || !sha.MatchString(t.Base) || (t.Previous != "" && !taskID.MatchString(t.Previous)) {
		return invalid("task identity or base")
	}
	if t.Status != "active" && t.Status != "completed" {
		return invalid("task status")
	}
	if t.Status == "active" && (t.Implementation != "" || t.Evidence != "") {
		return invalid("active task contains completion")
	}
	if t.Status == "completed" && (!sha.MatchString(t.Implementation) || len(t.Evidence) != 64) {
		return invalid("completed task lacks evidence")
	}
	return t.Plan.validate()
}
func (w Workflow) validate() error {
	if w.Schema != Version || w.Branch == "" || !sha.MatchString(w.ImportedBase) || (w.Active != "" && !taskID.MatchString(w.Active)) || (w.Completed != "" && !taskID.MatchString(w.Completed)) || (w.Active == "" && w.Completed == "") {
		return invalid(fmt.Sprintf("invalid workflow schema/state %d", w.Schema))
	}
	return nil
}
