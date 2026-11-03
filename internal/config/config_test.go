package config

import (
	"strings"
	"testing"
	"time"
)

const valid = `
version: 1
default_mode: code
modes:
  code: [test]
checks:
  test:
    run: go test ./...
    inputs: ["**/*.go"]
    timeout: 30s
`

func TestParseValid(t *testing.T) {
	c, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Checks["test"].TimeoutDuration(); got != 30*time.Second {
		t.Fatalf("timeout = %s", got)
	}
	names, err := c.ModeChecks("code")
	if err != nil || len(names) != 1 || names[0] != "test" {
		t.Fatalf("ModeChecks = %v, %v", names, err)
	}
	if _, err := c.ModeChecks("nope"); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

func TestParseReportsAllProblems(t *testing.T) {
	_, err := Parse([]byte(`
version: 2
default_mode: missing
modes:
  code: [test, ghost]
checks:
  test:
    run: ""
    timeout: soon
`))
	if err == nil {
		t.Fatal("invalid config accepted")
	}
	for _, want := range []string{"version must be 1", `"test": run is required`, `"test": inputs are required`,
		`invalid timeout "soon"`, `unknown check "ghost"`, `default_mode "missing"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	_, err := Parse([]byte(strings.Replace(valid, "timeout: 30s", "timout: 30s", 1)))
	if err == nil || !strings.Contains(err.Error(), "timout") {
		t.Fatalf("typo not rejected: %v", err)
	}
}

func TestParseRejectsUnsafeNamesAndPatterns(t *testing.T) {
	_, err := Parse([]byte(`
version: 1
default_mode: code
modes:
  code: ["../x", "a b"]
  empty: []
checks:
  "../x":
    run: "true"
    inputs: ["/abs/**", "../up/**", 'src\win', "**/*.{ts,tsx}", "src/"]
    timeout: 0s
  "a b":
    run: "true"
    inputs: ["**"]
    workdir: ../out
`))
	if err == nil {
		t.Fatal("unsafe config accepted")
	}
	for _, want := range []string{`check name "../x"`, `check name "a b"`, "relative to the repository root",
		"must not leave the repository", "use forward slashes", "braces and brackets", `"src/**"`,
		"positive duration", "workdir must be a path inside", `mode "empty" has no checks`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
}
