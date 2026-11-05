package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Chaebin-Park/naru/internal/config"
)

// PreToolInput is the subset of the PreToolUse hook payload naru uses.
type PreToolInput struct {
	Cwd       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
	} `json:"tool_input"`
}

// fileTools write the path in tool_input.file_path (or notebook_path).
var fileTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}

var (
	// `naru mode <name>` / `naru mode --clear` narrows what the gate requires.
	modeChange = regexp.MustCompile(`(^|[\s;&|(])naru\s+mode\s+(--clear|[A-Za-z0-9])`)
	// A redirection target: `> f`, `>> f`, `&> f`, `2> f`; not `2>&1`.
	redirectTarget = regexp.MustCompile(`(?:&|[0-9])?>>?\s*([^\s;|&<>]+)`)
	segmentSplit   = regexp.MustCompile(`&&|\|\||[;|&\n]`)
)

// Commands that modify every path they are given, the destination-writers
// (last argument), and interpreters that can write anything they mention.
var (
	writesArgs  = map[string]bool{"rm": true, "rmdir": true, "touch": true, "truncate": true, "chmod": true, "chown": true, "unlink": true, "shred": true, "mv": true, "tee": true}
	writesDest  = map[string]bool{"cp": true, "ln": true, "install": true, "rsync": true}
	interpreter = map[string]bool{"python": true, "python3": true, "node": true, "ruby": true, "perl": true, "php": true, "deno": true, "bun": true}
)

// bashWritesNaru reports whether cmd may modify .naru/. It is a heuristic over
// the command text: reading .naru/ (cat, grep, ls, a log) stays allowed.
func bashWritesNaru(cmd string) bool {
	if !strings.Contains(cmd, ".naru") {
		return false
	}
	for _, m := range redirectTarget.FindAllStringSubmatch(cmd, -1) {
		if strings.Contains(m[1], ".naru") {
			return true
		}
	}
	for _, seg := range segmentSplit.Split(cmd, -1) {
		if !strings.Contains(seg, ".naru") {
			continue
		}
		f := strings.Fields(redirectTarget.ReplaceAllString(seg, " "))
		for len(f) > 0 && (f[0] == "sudo" || f[0] == "env" || f[0] == "command" || strings.Contains(f[0], "=")) {
			f = f[1:]
		}
		if len(f) == 0 {
			continue
		}
		name := filepath.Base(f[0])
		args := f[1:]
		switch {
		case writesArgs[name]:
			return true
		case writesDest[name]:
			if len(args) > 0 && strings.Contains(args[len(args)-1], ".naru") {
				return true
			}
		case interpreter[name] || strings.HasPrefix(name, "python"):
			return true
		case name == "sed" || name == "gsed":
			for _, a := range args {
				if a == "-i" || strings.HasPrefix(a, "-i") || a == "--in-place" || strings.HasPrefix(a, "--in-place=") {
					return true
				}
			}
		case name == "git" && len(args) > 0:
			switch args[0] {
			case "checkout", "restore", "rm", "clean", "stash", "reset", "mv":
				return true
			}
		case name == "find":
			for _, a := range args {
				if a == "-delete" || a == "-exec" || a == "-execdir" {
					return true
				}
			}
		}
	}
	return false
}

// PreTool guards naru's own evidence. It blocks agent tool calls that would
// write into .naru/ (receipts, logs, config, stored mode) or change the mode,
// because either lets an agent satisfy the Stop gate without doing the work.
//
// This is a guard rail against an agent taking a shortcut, not a security
// boundary: a determined process can always write files. The authority is a
// clean re-run of `naru check` in CI, where receipts are never committed.
func PreTool(stdin io.Reader, stderr io.Writer, fallbackDir string) int {
	if strings.EqualFold(os.Getenv(DisableEnv), "off") {
		return Allow
	}
	var in PreToolInput
	malformedPatch := false
	data, _ := io.ReadAll(stdin)
	if err := json.Unmarshal(data, &in); err != nil {
		var envelope struct {
			ToolName string          `json:"tool_name"`
			Cwd      json.RawMessage `json:"cwd"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.ToolName == "apply_patch" {
			in.ToolName = envelope.ToolName
			in.Cwd = ""
			_ = json.Unmarshal(envelope.Cwd, &in.Cwd)
			malformedPatch = true
		} else {
			return Allow // not a tool payload naru understands
		}
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd = fallbackDir
	}
	root, err := findProject(os.Getenv(ProjectDirEnv), cwd, fallbackDir)
	if errors.Is(err, config.ErrNotFound) || err != nil {
		return Allow
	}
	if malformedPatch {
		_, _ = fmt.Fprintln(stderr, "naru: blocked apply_patch — malformed hook payload; expected tool_input.command containing a patch.")
		return Block
	}

	switch {
	case in.ToolName == "apply_patch":
		targets, err := patchTargets(in.ToolInput.Command)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "naru: blocked apply_patch — cannot determine patch targets: %v. Use a complete supported patch.\n", err)
			return Block
		}
		for _, target := range targets {
			if insideNaruDir(root, cwd, target) {
				_, _ = fmt.Fprintf(stderr, "naru: blocked apply_patch on %s — .naru/ holds the config and the evidence the Stop gate trusts. Run `naru check` to produce receipts; tell the user if the config needs to change.\n", target)
				return Block
			}
		}
	case fileTools[in.ToolName]:
		target := in.ToolInput.FilePath
		if target == "" {
			target = in.ToolInput.NotebookPath
		}
		if target != "" && insideNaruDir(root, cwd, target) {
			_, _ = fmt.Fprintf(stderr, "naru: blocked %s on %s — .naru/ holds the config and the evidence the Stop gate trusts, so agents must not edit it. If the config is wrong, tell the user what to change.\n", in.ToolName, target)
			return Block
		}
	case in.ToolName == "Bash":
		cmd := in.ToolInput.Command
		if modeChange.MatchString(cmd) {
			_, _ = fmt.Fprintln(stderr, "naru: blocked — the mode decides which checks must pass, so a human chooses it. Ask the user to run `naru mode <name>` if a different mode is appropriate.")
			return Block
		}
		if bashWritesNaru(cmd) {
			_, _ = fmt.Fprintln(stderr, "naru: blocked — this command may modify .naru/, which holds the config and the evidence the Stop gate trusts. Run `naru check` to produce receipts; tell the user if the config needs to change.")
			return Block
		}
	}
	return Allow
}

// insideNaruDir reports whether target (absolute, or relative to cwd) lies in
// <root>/.naru, resolving symlinks such as macOS's /tmp -> /private/tmp.
func insideNaruDir(root, cwd, target string) bool {
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, target)
	}
	naruDir := resolve(filepath.Join(root, config.Dir))
	t := resolve(target)
	rel, err := filepath.Rel(naruDir, t)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolve evaluates symlinks for the longest existing prefix of p.
func resolve(p string) string {
	p = filepath.Clean(p)
	var rest []string
	for cur := p; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(append([]string{real}, rest...)...)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}
