package surface

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSnapshotValidation(t *testing.T) {
	s := build(t, request(), fixture(t, "routes.json"))
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeSnapshot(data, s.SurfaceID); err != nil || got.Digest != s.Digest {
		t.Fatalf("roundtrip: %+v %v", got, err)
	}
	for name, b := range map[string][]byte{
		"digest":          bytes.Replace(data, []byte(s.Digest), []byte(strings.Repeat("0", 64)), 1),
		"content":         bytes.Replace(data, []byte(`"GET"`), []byte(`"POST"`), 1),
		"unknown field":   append([]byte(`{"unknown":1,`), data[1:]...),
		"duplicate field": append([]byte(`{"schema":1,`), data[1:]...),
		"missing field":   bytes.Replace(data, []byte(`"policy":"surface-json-v1",`), nil, 1),
		"trailing":        append(append([]byte{}, data...), []byte(" {}")...),
		"null":            []byte("null"),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := DecodeSnapshot(b, s.SurfaceID); got != nil || !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted %s: %+v %v", name, got, err)
			}
		})
	}
	if _, err := DecodeSnapshot(data, "other"); err == nil {
		t.Fatal("cross-surface snapshot accepted")
	}
	// HTML escaping can expand canonical bytes beyond the stdout limit.
	large := []byte(`{"schema":1,"items":[{"id":"x","value":{"text":"` + strings.Repeat("<", 200000) + `"}}]}`)
	s = build(t, request(), large)
	data, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSnapshot(data, s.SurfaceID); err != nil {
		t.Fatal("valid expanded snapshot rejected", err)
	}
}

func TestSnapshotDepthBoundary(t *testing.T) {
	output := []byte(`{"schema":1,"items":[{"id":"x","value":{"deep":` + strings.Repeat("[", 60) + "0" + strings.Repeat("]", 60) + `}}]}`)
	s := build(t, request(), output)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSnapshot(data, s.SurfaceID); err != nil {
		t.Fatal("valid output cannot survive snapshot envelope", err)
	}
}
