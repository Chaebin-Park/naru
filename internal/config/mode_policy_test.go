package config

import "testing"

func TestModePolicy(t *testing.T) {
	base := "version: 1\ndefault_mode: code\nmodes: {code: [c]}\nchecks: {c: {run: 'true', inputs: [input]}}\n"
	for _, tc := range []struct {
		mapping string
		valid   bool
	}{
		{"", true}, {"mode_policy: {code: true}\n", true}, {"mode_policy: {code: false}\n", true}, {"mode_policy: {unknown: true}\n", false},
	} {
		if _, err := Parse([]byte(base + tc.mapping)); (err == nil) != tc.valid {
			t.Fatalf("%q: %v", tc.mapping, err)
		}
	}
}
