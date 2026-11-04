// Package surface defines the versioned, in-memory extractor contract.
// It does not execute extractors or read or replace approved baselines.
package surface

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
)

const Schema = 1
const Policy = "surface-json-v1"
const MaxOutputBytes = 1 << 20

var ErrInvalid = errors.New("invalid surface contract")
var ErrExtraction = errors.New("surface extraction failed")

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+\-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Request binds extraction to the caller's observed inputs and extractor version.
// InputDigest is supplied by the future execution adapter, not by the extractor.
type Request struct {
	Schema           int    `json:"schema"`
	SurfaceID        string `json:"surface_id"`
	ExtractorVersion string `json:"extractor_version"`
	InputDigest      string `json:"input_digest"`
}

// RequestBytes validates and encodes the single JSON value sent on stdin.
func RequestBytes(r Request) ([]byte, error) {
	if r.Schema != Schema || !identifier.MatchString(r.SurfaceID) || !identifier.MatchString(r.ExtractorVersion) || !digestPattern.MatchString(r.InputDigest) {
		return nil, fmt.Errorf("%w: request identity or input digest", ErrInvalid)
	}
	return json.Marshal(r)
}

// Snapshot separates input freshness from the digest of the observed contract.
// Canonical contains the exact bytes hashed by Digest; neither field is evidence
// that a process ran or that a human approved this contract.
type Snapshot struct {
	Schema           int             `json:"schema"`
	SurfaceID        string          `json:"surface_id"`
	ExtractorVersion string          `json:"extractor_version"`
	InputDigest      string          `json:"input_digest"`
	Policy           string          `json:"policy"`
	Canonical        json.RawMessage `json:"canonical"`
	Digest           string          `json:"digest"`
}

type item struct {
	ID    string         `json:"id"`
	Value map[string]any `json:"value"`
}

type document struct {
	Schema           int    `json:"schema"`
	Policy           string `json:"policy"`
	SurfaceID        string `json:"surface_id"`
	ExtractorVersion string `json:"extractor_version"`
	Items            []item `json:"items"`
}

// Build accepts output only after successful extraction. Callers must pass any
// process, cancellation, capture or output-limit error as runErr. An explicit
// {"schema":1,"items":[]} is valid; absent output or an error never means empty.
func Build(r Request, stdout []byte, runErr error) (*Snapshot, error) {
	if runErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrExtraction, runErr)
	}
	if _, err := RequestBytes(r); err != nil {
		return nil, err
	}
	value, err := parseOutput(stdout)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	obj, ok := value.(map[string]any)
	if !ok || len(obj) != 2 || obj["schema"] != json.Number("1") {
		return nil, fmt.Errorf("%w: expected schema 1 and items", ErrInvalid)
	}
	rows, ok := obj["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: items must be an explicit array", ErrInvalid)
	}
	items := make([]item, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		entry, ok := row.(map[string]any)
		if !ok || len(entry) != 2 {
			return nil, fmt.Errorf("%w: item must contain only id and value", ErrInvalid)
		}
		id, ok := entry["id"].(string)
		if !ok || !identifier.MatchString(id) || seen[id] {
			return nil, fmt.Errorf("%w: invalid or duplicate item id", ErrInvalid)
		}
		payload, ok := entry["value"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: item value must be an object", ErrInvalid)
		}
		seen[id] = true
		items = append(items, item{ID: id, Value: payload})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	canonical, err := json.Marshal(document{Schema, Policy, r.SurfaceID, r.ExtractorVersion, items})
	if err != nil {
		return nil, fmt.Errorf("encode surface: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return &Snapshot{Schema, r.SurfaceID, r.ExtractorVersion, r.InputDigest, Policy, canonical, hex.EncodeToString(sum[:])}, nil
}
