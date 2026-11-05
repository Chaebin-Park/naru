package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/Chaebin-Park/naru/internal/surface"
)

// FindingID binds identity to the rule, stable location and tool fingerprint.
func FindingID(rule, location, fingerprint string) string {
	b, _ := json.Marshal([]string{rule, location, fingerprint})
	return "finding:" + hash(b)
}

// uniqueJSON rejects duplicate object fields and excessive nesting before decoding.
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrInvalid
	}
	tok, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return ErrInvalid
			}
			seen[s] = true
			if e = uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return ErrInvalid
		}
	case '[':
		for d.More() {
			if err = uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
		end, e := d.Token()
		if e != nil || end != json.Delim(']') {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func decode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := uniqueJSON(d, 0); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}
func fields(data []byte, keys ...string) error {
	var m map[string]json.RawMessage
	if err := decode(data, &m); err != nil {
		return err
	}
	if len(m) != len(keys) {
		return ErrInvalid
	}
	for _, k := range keys {
		if m[k] == nil {
			return ErrInvalid
		}
		if bytes.Equal(bytes.TrimSpace(m[k]), []byte("null")) && k != "previous_revision" && k != "findings" {
			return ErrInvalid
		}
	}
	return nil
}
func validateFinding(f Finding) bool {
	return text(f.RuleID, 64) && text(f.StableLocation, 512) && validDigest(f.Fingerprint) && relative(f.File) && text(f.File, 1024) && f.Line > 0 && text(f.Message, 2048) && f.ID == FindingID(f.RuleID, f.StableLocation, f.Fingerprint)
}
func sortedFindings(fs []Finding) bool {
	if fs == nil {
		return false
	}
	last := ""
	for _, f := range fs {
		if !validateFinding(f) || f.ID <= last {
			return false
		}
		last = f.ID
	}
	return true
}

// ParseFindings requires explicit completeness for every indexed rule.
func ParseFindings(snapshot *surface.Snapshot, index *Index) ([]Finding, error) {
	if snapshot == nil || index == nil || snapshot.SurfaceID != index.Surface {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	if _, err = surface.DecodeSnapshot(raw, index.Surface); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	var doc struct {
		Schema           int    `json:"schema"`
		Policy           string `json:"policy"`
		SurfaceID        string `json:"surface_id"`
		ExtractorVersion string `json:"extractor_version"`
		Items            []struct {
			ID    string          `json:"id"`
			Value json.RawMessage `json:"value"`
		} `json:"items"`
	}
	if err = decode(snapshot.Canonical, &doc); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	coverage := map[string]bool{}
	findings := []Finding{}
	for _, row := range doc.Items {
		var kind struct {
			Kind string `json:"kind"`
		}
		if err = json.Unmarshal(row.Value, &kind); err != nil {
			return nil, ErrInvalid
		}
		switch kind.Kind {
		case "coverage":
			if err = fields(row.Value, "kind", "rule_id", "complete"); err != nil {
				return nil, err
			}
			var v struct {
				Kind     string `json:"kind"`
				RuleID   string `json:"rule_id"`
				Complete bool   `json:"complete"`
			}
			if err = decode(row.Value, &v); err != nil {
				return nil, err
			}
			if _, ok := index.Rules[v.RuleID]; !ok || !v.Complete || coverage[v.RuleID] || row.ID != "coverage:"+v.RuleID {
				return nil, ErrInvalid
			}
			coverage[v.RuleID] = true
		case "finding":
			if err = fields(row.Value, "kind", "rule_id", "stable_location", "fingerprint", "file", "line", "message"); err != nil {
				return nil, err
			}
			var v struct {
				Kind           string `json:"kind"`
				RuleID         string `json:"rule_id"`
				StableLocation string `json:"stable_location"`
				Fingerprint    string `json:"fingerprint"`
				File           string `json:"file"`
				Line           int    `json:"line"`
				Message        string `json:"message"`
			}
			if err = decode(row.Value, &v); err != nil {
				return nil, err
			}
			f := Finding{row.ID, v.RuleID, v.StableLocation, v.Fingerprint, v.File, v.Line, v.Message}
			if _, ok := index.Rules[f.RuleID]; !ok || !validateFinding(f) || seen[f.ID] {
				return nil, ErrInvalid
			}
			seen[f.ID] = true
			findings = append(findings, f)
		default:
			return nil, ErrInvalid
		}
	}
	if len(coverage) != len(index.Rules) {
		return nil, fmt.Errorf("%w: missing complete rule coverage", ErrNotReady)
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	return findings, nil
}
