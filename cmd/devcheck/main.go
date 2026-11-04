// devcheck is a temporary, repository-local development scanner, not the naru CLI.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const toolDir = ".naru/state/devtools/bin"

type issue struct {
	ID       string `json:"id"`
	Tool     string `json:"tool"`
	Rule     string `json:"rule"`
	Priority string `json:"priority"`
	Location string `json:"location"`
	Message  string `json:"message"`
}
type report struct {
	ToolVersions map[string]string `json:"tool_versions"`
	Started      time.Time         `json:"started"`
	Finished     time.Time         `json:"finished"`
	InputHash    string            `json:"input_hash"`
	Complete     bool              `json:"complete"`
	Issues       []issue           `json:"issues"`
	Errors       []string          `json:"errors"`
}

func main() { os.Exit(mainCode()) }
func mainCode() int {
	watch := flag.Bool("watch", false, "rescan on input changes and hourly for vulnerability database updates")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: devcheck [-watch]")
		return 64
	}
	if _, err := os.Stat("go.mod"); err != nil {
		fmt.Fprintln(os.Stderr, "run devcheck from the repository root")
		return 64
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	previous := ""
	last := time.Time{}
	for {
		hash, err := inputHash()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 70
		}
		if !*watch || hash != previous || time.Since(last) >= time.Hour {
			code := scan(ctx, hash)
			if !*watch {
				return code
			}
			previous = hash
			last = time.Now()
		}
		select {
		case <-ctx.Done():
			return 130
		case <-time.After(2 * time.Second):
		}
	}
}

func scan(ctx context.Context, hash string) int {
	r := report{ToolVersions: map[string]string{"go": runtime.Version(), "govulncheck": "v1.9.0"}, Started: time.Now().UTC(), InputHash: hash, Complete: true, Issues: []issue{}, Errors: []string{}}
	if err := os.MkdirAll(".naru/state", 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 70
	}
	dir, err := os.MkdirTemp(".naru/state", "devcheck-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 70
	}
	tasks := []struct {
		name  string
		args  []string
		parse func([]byte) ([]issue, error)
	}{
		{"golangci-lint", []string{"run", "--output.json.path=stdout", "--output.text.path=", "--show-stats=false", "--uniq-by-line=false", "./..."}, parseLint},
		{"govulncheck", []string{"-json", "-test", "./..."}, parseVuln},
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			r.Errors = append(r.Errors, "scan cancelled")
			break
		}
		if task.name == "golangci-lint" {
			versionCtx, stopVersion := context.WithTimeout(ctx, 10*time.Second)
			version, versionErr := exec.CommandContext(versionCtx, filepath.Join(toolDir, "golangci-lint"), "version").CombinedOutput() // #nosec G204 -- Only pinned local tool names and fixed argument lists are executed; no shell is used.
			stopVersion()
			r.ToolVersions[task.name] = strings.TrimSpace(string(version))
			if versionErr != nil || !strings.Contains(string(version), "version 2.15.0 ") {
				r.Errors = append(r.Errors, "golangci-lint v2.15.0 required: "+string(version))
				continue
			}
		}
		taskCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
		cmd := exec.CommandContext(taskCtx, filepath.Join(toolDir, task.name), task.args...) // #nosec G204 -- Only pinned local tool names and fixed argument lists are executed; no shell is used.
		cmd.WaitDelay = 5 * time.Second
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, runErr := cmd.Output()
		cancel()
		if err := os.WriteFile(filepath.Join(dir, task.name+".json"), data, 0o600); err != nil {
			r.Errors = append(r.Errors, err.Error())
		}
		if err := os.WriteFile(filepath.Join(dir, task.name+".stderr"), stderr.Bytes(), 0o600); err != nil {
			r.Errors = append(r.Errors, err.Error())
		}
		findings, parseErr := task.parse(data)
		r.Issues = append(r.Issues, findings...)
		// Lint uses exit 1 for findings. govulncheck JSON requires parsing findings:
		// a zero exit code alone does not mean that no vulnerabilities were found.
		var exitErr *exec.ExitError
		lintFindings := task.name == "golangci-lint" && errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 && len(findings) > 0
		if runErr != nil && !lintFindings {
			r.Errors = append(r.Errors, fmt.Sprintf("%s: %v; %s", task.name, runErr, strings.TrimSpace(stderr.String())))
		}
		if parseErr != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("%s output: %v", task.name, parseErr))
		}
	}
	after, err := inputHash()
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
	} else if after != hash {
		r.Errors = append(r.Errors, "inputs changed during scan; rerun required")
	}
	r.Complete = len(r.Errors) == 0
	r.Finished = time.Now().UTC()
	// Keep each scan separately: failed scans must not erase earlier evidence.
	r.Issues = deduplicate(r.Issues)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 70
	}
	if err = os.WriteFile(filepath.Join(dir, "report.json"), data, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 70
	}
	var md strings.Builder
	fmt.Fprintf(&md, "# Development findings\n\nStarted: %s\n\nComplete: %t\n\n", r.Started.Format(time.RFC3339), r.Complete)
	for _, x := range r.Issues {
		fmt.Fprintf(&md, "## %s — %s\n\nTool: %s / %s\n\nLocation: %s\n\n%s\n\nStatus: open; requires triage.\n\n", x.ID, x.Priority, x.Tool, x.Rule, x.Location, x.Message)
		fmt.Printf("%s %s %s %s: %s\n", x.ID, x.Priority, x.Rule, x.Location, x.Message)
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&md, "Scan error: %s\n\n", e)
		fmt.Fprintln(os.Stderr, e)
	}
	if err = os.WriteFile(filepath.Join(dir, "issues.md"), []byte(md.String()), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 70
	}
	fmt.Printf("devcheck: complete=%t, findings=%d; report: %s\n", r.Complete, len(r.Issues), filepath.Join(dir, "issues.md"))
	if !r.Complete {
		return 70
	}
	if len(r.Issues) > 0 {
		return 1
	}
	return 0
}

func parseLint(data []byte) ([]issue, error) {
	var raw struct {
		Issues *[]struct {
			FromLinter, Text string
			Pos              struct {
				Filename string
				Line     int
			}
		}
		Report struct{ Error string }
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Issues == nil {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
		if _, ok := fields["Issues"]; !ok {
			return nil, errors.New("missing Issues field")
		}
		empty := []struct {
			FromLinter, Text string
			Pos              struct {
				Filename string
				Line     int
			}
		}{}
		raw.Issues = &empty
	}
	if raw.Report.Error != "" {
		return nil, errors.New(raw.Report.Error)
	}
	var out []issue
	for _, x := range *raw.Issues {
		if x.FromLinter == "" || x.Text == "" {
			return nil, errors.New("invalid lint finding")
		}
		priority := "review"
		if x.FromLinter == "gosec" {
			priority = "security-review"
		}
		out = append(out, issue{Tool: "golangci-lint", Rule: x.FromLinter, Priority: priority, Location: fmt.Sprintf("%s:%d", x.Pos.Filename, x.Pos.Line), Message: x.Text})
	}
	return out, nil
}

func parseVuln(data []byte) ([]issue, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	configured := false
	sbom := false
	var out []issue
	for {
		var msg struct {
			Config *struct {
				ScannerName    string `json:"scanner_name"`
				ScannerVersion string `json:"scanner_version"`
				ScanLevel      string `json:"scan_level"`
			} `json:"config"`
			SBOM    json.RawMessage `json:"SBOM"`
			Finding *struct {
				OSV   string `json:"osv"`
				Fixed string `json:"fixed_version"`
				Trace []struct {
					Module, Package, Function string
					Position                  *struct {
						Filename string
						Line     int
					}
				} `json:"trace"`
			} `json:"finding"`
		}
		err := dec.Decode(&msg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		if msg.Config != nil {
			if msg.Config.ScannerName != "govulncheck" || msg.Config.ScannerVersion != "v1.9.0" || msg.Config.ScanLevel != "symbol" {
				return out, errors.New("unexpected scanner version or scan level")
			}
			configured = true
		}
		if len(msg.SBOM) > 0 && string(msg.SBOM) != "null" {
			sbom = true
		}
		if msg.Finding == nil {
			continue
		}
		f := msg.Finding
		if f.OSV == "" || len(f.Trace) == 0 {
			return out, errors.New("invalid vulnerability finding")
		}
		frame := f.Trace[0]
		priority := "dependency-review"
		if frame.Function != "" {
			priority = "reachable-vulnerability"
		}
		location := frame.Module
		for _, fr := range f.Trace {
			if fr.Position != nil {
				location = fmt.Sprintf("%s:%d", fr.Position.Filename, fr.Position.Line)
			}
		}
		out = append(out, issue{Tool: "govulncheck", Rule: f.OSV, Priority: priority, Location: location, Message: fmt.Sprintf("%s %s; fixed version: %s; https://pkg.go.dev/vuln/%s", frame.Package, frame.Function, f.Fixed, f.OSV)})
	}
	if !configured || !sbom {
		return out, errors.New("missing scanner configuration or SBOM")
	}
	return out, nil
}

func deduplicate(in []issue) []issue {
	out := []issue{}
	seen := map[string]bool{}
	for _, x := range in {
		sum := sha256.Sum256([]byte(x.Tool + "\x00" + x.Rule + "\x00" + x.Location + "\x00" + x.Message))
		x.ID = "DEV-" + hex.EncodeToString(sum[:6])
		if !seen[x.ID] {
			seen[x.ID] = true
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func inputHash() (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "diagrams" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") && p != "go.mod" && p != "go.sum" && p != ".golangci.yml" && p != "scripts/devcheck.sh" {
			return nil
		}
		data, err := os.ReadFile(p) // #nosec G304 G122 -- Reads local development sources for change detection, not untrusted remote paths. Change detection is not a sandbox or snapshot; concurrent symlink changes remain a documented limitation.
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(h, "%d:%s%d:", len(p), p, len(data))
		if err != nil {
			return err
		}
		_, err = h.Write(data)
		return err
	})
	return hex.EncodeToString(h.Sum(nil)), err
}
