package surface

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DecodeSnapshot validates persisted identity and recomputes canonical bytes and
// digest. Compact canonical bytes must be preserved by the storage encoder.
func DecodeSnapshot(data []byte, name string) (*Snapshot, error) {
	value, err := parseJSON(data, 8<<20, 65)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	obj, ok := value.(map[string]any)
	if !ok || len(obj) != 7 {
		return nil, ErrInvalid
	}
	for _, key := range []string{"schema", "surface_id", "extractor_version", "input_digest", "policy", "canonical", "digest"} {
		if obj[key] == nil {
			return nil, ErrInvalid
		}
	}
	var s Snapshot
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	r := Request{s.Schema, s.SurfaceID, s.ExtractorVersion, s.InputDigest}
	if _, err := RequestBytes(r); err != nil {
		return nil, err
	}
	if s.SurfaceID != name || s.Policy != Policy {
		return nil, ErrInvalid
	}
	doc, ok := obj["canonical"].(map[string]any)
	if !ok || len(doc) != 5 || doc["schema"] != json.Number("1") || doc["policy"] != Policy || doc["surface_id"] != s.SurfaceID || doc["extractor_version"] != s.ExtractorVersion {
		return nil, ErrInvalid
	}
	rebuilt, err := buildValue(r, map[string]any{"schema": json.Number("1"), "items": doc["items"]})
	if err != nil {
		return nil, err
	}
	if s.Digest != rebuilt.Digest || !bytes.Equal(s.Canonical, rebuilt.Canonical) {
		return nil, fmt.Errorf("%w: snapshot digest or canonical bytes", ErrInvalid)
	}
	return rebuilt, nil
}
