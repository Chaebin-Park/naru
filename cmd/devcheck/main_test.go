package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintFindingsAndInvalidOutput(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		count     int
		bad       bool
	}{
		{"finding", `{"Issues":[{"FromLinter":"errcheck","Text":"unchecked close","Pos":{"Filename":"x.go","Line":4}}]}`, 1, false},
		{"clean", `{"Issues":[]}`, 0, false},
		{"null-clean", `{"Issues":null}`, 0, false},
		{"missing", "{}", 0, true},
		{"truncated", `{"Issues":[`, 0, true},
		{"trailing-summary", `{"Issues":[]} 4 issues`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLint([]byte(tc.raw))
			if (err != nil) != tc.bad || len(got) != tc.count {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

const vulnConfig = `{"config":{"scanner_name":"govulncheck","scanner_version":"v1.9.0","scan_level":"symbol"}}` + "\n" + `{"SBOM":{"modules":[]}}` + "\n"

func TestVulnerabilityJSONFindingsEvenOnSuccessfulProcess(t *testing.T) {
	raw := vulnConfig + `{"finding":{"osv":"GO-TEST-0001","fixed_version":"v1.2.3","trace":[{"module":"example.org/lib","package":"example.org/lib","function":"Bad"},{"module":"example.org/app","position":{"filename":"main.go","line":9}}]}}`
	got, err := parseVuln([]byte(raw))
	if err != nil || len(got) != 1 || got[0].Priority != "reachable-vulnerability" || got[0].Location != "main.go:9" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestVulnerabilityIncompleteAndDependencyOnly(t *testing.T) {
	for _, raw := range []string{"", `{"config":{}}`, vulnConfig + `{"finding":`, strings.Replace(vulnConfig, "v1.9.0", "v0.0.0", 1)} {
		if _, err := parseVuln([]byte(raw)); err == nil {
			t.Fatalf("accepted incomplete output: %s", raw)
		}
	}
	got, err := parseVuln([]byte(vulnConfig + `{"finding":{"osv":"GO-TEST-0001","trace":[{"module":"example.org/lib"}]}}`))
	if err != nil || len(got) != 1 || got[0].Priority != "dependency-review" {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := parseVuln([]byte(vulnConfig)); err != nil || len(got) != 0 {
		t.Fatalf("clean: %v, %v", got, err)
	}
}

func TestStableIssueIDsAndDeduplication(t *testing.T) {
	x := issue{Tool: "lint", Rule: "rule", Location: "x.go:1", Message: "problem"}
	first := deduplicate([]issue{x, x})
	second := deduplicate([]issue{x})
	if len(first) != 1 || first[0].ID != second[0].ID || !strings.HasPrefix(first[0].ID, "DEV-") {
		t.Fatalf("unstable IDs: %v %v", first, second)
	}
}

// A tool execution failure must remain an incomplete scan even if its JSON is clean.
func TestFailedScannerCannotPass(t *testing.T) {
	root := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Error(err)
		}
	})
	if err = os.WriteFile("go.mod", []byte("module example.org/test\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := inputHash()
	if err != nil {
		t.Fatal(err)
	}
	if code := scan(context.Background(), hash); code != 70 {
		t.Fatalf("missing tools exit=%d", code)
	}
	paths, err := filepath.Glob(".naru/state/devcheck-*/report.json")
	if err != nil || len(paths) != 1 {
		t.Fatalf("reports: %v %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var r report
	if err = json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if r.Complete || len(r.Errors) == 0 {
		t.Fatalf("failed tools reported clean: %+v", r)
	}
}
