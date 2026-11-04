package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequirementsValidationAndFingerprint(t *testing.T) {
	for _, req := range []string{`{tools: [../go]}`, `{tools: ['go test']}`, `{tools: [go, go]}`, `{os: [macOS]}`, `{os: [linux, linux]}`, `{tool: [go]}`, `[go]`, `{tools: [` + strings.Repeat("a", 257) + `]}`} {
		if _, err := Parse([]byte(valid + "    requires: " + req + "\n")); err == nil {
			t.Fatalf("accepted %s", req)
		}
	}
	cfg, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	ch := cfg.Checks["test"]
	old, _ := json.Marshal(struct {
		Run     string   `json:"run"`
		Workdir string   `json:"workdir"`
		Inputs  []string `json:"inputs"`
		Exclude []string `json:"exclude"`
	}{ch.Run, ch.Workdir, ch.Inputs, cfg.Exclude})
	if cfg.Fingerprint("test") != string(old) {
		t.Fatal("omitted requirements changed existing fingerprints")
	}
	with, err := Parse([]byte(valid + "    requires: {tools: [go, git], os: [darwin, linux]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := Parse([]byte(valid + "    requires: {tools: [git, go], os: [linux, darwin]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if with.Fingerprint("test") == cfg.Fingerprint("test") {
		t.Fatal("requirements did not affect fingerprint")
	}
	if with.Fingerprint("test") != reversed.Fingerprint("test") {
		t.Fatal("equivalent requirement order changed fingerprint")
	}
	if strings.Join(with.Checks["test"].Requires.Tools, ",") != "go,git" {
		t.Fatal("fingerprint mutated config")
	}
}
