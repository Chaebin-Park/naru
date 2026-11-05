// Package initcmd scaffolds .naru/ for a repository.
package initcmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Chaebin-Park/naru/internal/config"
)

// stack is a detected build system and the first check naru proposes for it.
// Inputs default to everything ("**") minus build outputs: over-invalidating a
// receipt only costs a re-run, while missing an input lets stale work pass.
type stack struct {
	name    string
	marker  string // file whose presence identifies the stack
	run     string
	exclude []string
}

// Order matters: the first match wins. Commands are a starting point; the
// user is expected to review them.
var stacks = []stack{
	{"go", "go.mod", "go test ./...", []string{"bin/**", "dist/**"}},
	{"gradle-kts", "build.gradle.kts", "./gradlew test", []string{"**/build/**", ".gradle/**", "**/.cxx/**", ".idea/**", "local.properties"}},
	{"gradle", "build.gradle", "./gradlew test", []string{"**/build/**", ".gradle/**", "**/.cxx/**", ".idea/**", "local.properties"}},
	{"maven", "pom.xml", "mvn -q test", []string{"**/target/**", ".idea/**"}},
	{"swift-package", "Package.swift", "swift test", []string{".build/**", ".swiftpm/**", "**/DerivedData/**"}},
	{"node", "package.json", "npm test", []string{"**/node_modules/**", "dist/**", "build/**", "coverage/**", ".next/**"}},
	{"python", "pyproject.toml", "python3 -m pytest", []string{"**/__pycache__/**", ".venv/**", "**/.pytest_cache/**", "dist/**", "build/**"}},
	{"rust", "Cargo.toml", "cargo test", []string{"target/**"}},
}

// Result reports what init did.
type Result struct {
	Root     string
	Stack    string // "" when nothing was detected
	Created  []string
	Existing bool
}

// Run writes .naru/config.yaml and .naru/.gitignore. It refuses to overwrite an
// existing config unless force is set.
func Run(root string, force bool) (Result, error) {
	res := Result{Root: root}
	cfgPath := filepath.Join(root, config.Dir, config.FileName)
	if _, err := os.Stat(cfgPath); err == nil && !force {
		res.Existing = true
		return res, fmt.Errorf("%s already exists (use --force to overwrite)", filepath.Join(config.Dir, config.FileName))
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	if err := os.MkdirAll(filepath.Join(root, config.Dir), 0o755); err != nil { // #nosec G301 -- Scaffold directory holds intentionally shareable source configuration.
		return res, err
	}
	st := detect(root)
	res.Stack = st.name
	if err := os.WriteFile(cfgPath, []byte(render(st)), 0o644); err != nil { // #nosec G306 -- Generated config and ignore rules are shareable source files, not private execution state.
		return res, err
	}
	res.Created = append(res.Created, filepath.Join(config.Dir, config.FileName))

	gi := filepath.Join(root, config.Dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, fs.ErrNotExist) {
		// Receipts, logs and local mode are per-working-copy evidence, not source.
		if err := os.WriteFile(gi, []byte("receipts/\nlogs/\nstate/\nrelay/\n"), 0o644); err != nil { // #nosec G306 -- Generated config and ignore rules are shareable source files, not private execution state.
			return res, err
		}
		res.Created = append(res.Created, filepath.Join(config.Dir, ".gitignore"))
	}
	return res, nil
}

func detect(root string) stack {
	for _, s := range stacks {
		if _, err := os.Stat(filepath.Join(root, s.marker)); err == nil {
			return s
		}
	}
	return stack{}
}

func render(s stack) string {
	run := s.run
	header := fmt.Sprintf("# Detected stack: %s. Review the command and inputs before relying on them.\n", s.name)
	if s.name == "" {
		// Fail closed until someone configures a real check.
		run = `echo "naru: configure checks in .naru/config.yaml" >&2; exit 1`
		header = "# No known build file detected. Replace the placeholder check below.\n"
	}
	quoted := make([]string, len(s.exclude))
	for i, ex := range s.exclude {
		quoted[i] = fmt.Sprintf("%q", ex)
	}
	return header + `# naru config — https://github.com/Chaebin-Park/naru
version: 1

# Mode used when none is selected with --mode, $NARU_MODE or ` + "`naru mode <name>`" + `.
default_mode: code

# Each mode lists the checks that must pass for the current files before an agent may finish.
modes:
  code: [test]

checks:
  test:
    run: ` + fmt.Sprintf("%q", run) + `
    # Receipts are bound to the content of these files (root-relative globs, "**" = any depth).
    # Everything by default: narrowing this is safe only if the check truly ignores the rest.
    inputs: ["**"]
    timeout: 10m

# Globs removed from every check's inputs (build outputs, caches, IDE state).
exclude: [` + strings.Join(quoted, ", ") + `]
`
}
