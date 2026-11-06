package config

import "testing"

func TestModeSurfaces(t *testing.T) {
	base := "version: 1\ndefault_mode: code\nmodes: {code: [c]}\nchecks: {c: {run: 'true', inputs: [input]}}\nsurfaces: {api: {run: 'true', inputs: [input], extractor_version: v1}}\n"
	for _, tc := range []struct {
		mapping string
		valid   bool
	}{
		{"", true}, {"mode_surfaces: {code: [api]}\n", true},
		{"mode_surfaces: {unknown: [api]}\n", false},
		{"mode_surfaces: {code: [unknown]}\n", false},
		{"mode_surfaces: {code: [api, api]}\n", false},
	} {
		if _, err := Parse([]byte(base + tc.mapping)); (err == nil) != tc.valid {
			t.Fatalf("%q: %v", tc.mapping, err)
		}
	}
}
