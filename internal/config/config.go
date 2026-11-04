// Package config loads and validates .naru/config.yaml.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Dir is the per-repository naru directory.
const Dir = ".naru"

// FileName is the config file inside Dir.
const FileName = "config.yaml"

// DefaultTimeout applies when a check does not set one.
const DefaultTimeout = 10 * time.Minute

// Config is the parsed .naru/config.yaml.
type Config struct {
	Version     int                 `yaml:"version"`
	DefaultMode string              `yaml:"default_mode"`
	Modes       map[string][]string `yaml:"modes"`
	Checks      map[string]Check    `yaml:"checks"`
	// Exclude lists glob patterns removed from every check's inputs.
	Exclude []string `yaml:"exclude"`
	Gate    Gate     `yaml:"gate"`
}

// DefaultMaxRepeats is how many identical consecutive Stop blocks naru issues
// before it lets the agent stop (reporting that completion was not verified).
const DefaultMaxRepeats = 3

// Gate tunes `naru gate stop`.
type Gate struct {
	// MaxRepeats bounds identical consecutive blocks with no progress (nothing
	// changed between them). 0 means never give up. Unset means DefaultMaxRepeats.
	MaxRepeats *int `yaml:"max_repeats"`
}

// GateMaxRepeats returns the effective repeat limit.
func (c *Config) GateMaxRepeats() int {
	if c == nil || c.Gate.MaxRepeats == nil {
		return DefaultMaxRepeats
	}
	return *c.Gate.MaxRepeats
}

// Check is one runnable verification.
type Check struct {
	// Run is executed with `sh -c` from the repository root (or Workdir).
	Run string `yaml:"run"`
	// Inputs are root-relative glob patterns ("**" matches any depth).
	// The receipt is bound to the content hash of the matched files.
	Inputs   []string     `yaml:"inputs"`
	Timeout  string       `yaml:"timeout"`
	Workdir  string       `yaml:"workdir"`
	Requires Requirements `yaml:"requires"`
}

// TimeoutDuration returns the parsed timeout or DefaultTimeout.
func (c Check) TimeoutDuration() time.Duration {
	if c.Timeout == "" {
		return DefaultTimeout
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return DefaultTimeout
	}
	return d
}

// ErrNotFound means no .naru/config.yaml exists at or above the start dir.
var ErrNotFound = errors.New("naru config not found")

// FindRoot walks up from start until it finds a directory containing .naru/config.yaml.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, Dir, FileName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

// Load reads and validates the config under root.
func Load(root string) (*Config, error) {
	path := filepath.Join(root, Dir, FileName)
	data, err := os.ReadFile(path) // #nosec G304 -- Reads the selected local project config; caller chooses the repository root.
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes and validates config bytes. Unknown keys are rejected so typos surface.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse %s/%s: %w", Dir, FileName, err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate reports every structural problem at once.
func (c *Config) Validate() error {
	var errs []error
	if c.Version != 1 {
		errs = append(errs, fmt.Errorf("version must be 1 (got %d)", c.Version))
	}
	if len(c.Checks) == 0 {
		errs = append(errs, errors.New("at least one check is required"))
	}
	for _, name := range sortedKeys(c.Checks) {
		ch := c.Checks[name]
		if err := ch.Requires.validate(); err != nil {
			errs = append(errs, fmt.Errorf("check %q: %w", name, err))
		}
		if !NamePattern.MatchString(name) {
			errs = append(errs, fmt.Errorf("check name %q must match %s", name, NamePattern))
		}
		for _, p := range ch.Inputs {
			if err := validatePattern(p); err != nil {
				errs = append(errs, fmt.Errorf("check %q: input %q: %v", name, p, err))
			}
		}
		if ch.Run == "" {
			errs = append(errs, fmt.Errorf("check %q: run is required", name))
		}
		if len(ch.Inputs) == 0 {
			errs = append(errs, fmt.Errorf("check %q: inputs are required (receipts are bound to them)", name))
		}
		if ch.Timeout != "" {
			if d, err := time.ParseDuration(ch.Timeout); err != nil || d <= 0 {
				errs = append(errs, fmt.Errorf("check %q: invalid timeout %q (use a positive duration such as 10m)", name, ch.Timeout))
			}
		}
		if ch.Workdir != "" {
			if filepath.IsAbs(ch.Workdir) || strings.Contains(ch.Workdir, "..") {
				errs = append(errs, fmt.Errorf("check %q: workdir must be a path inside the repository", name))
			}
		}
	}
	if c.Gate.MaxRepeats != nil && *c.Gate.MaxRepeats < 0 {
		errs = append(errs, errors.New("gate.max_repeats must be 0 (never give up) or positive"))
	}
	if len(c.Modes) == 0 {
		errs = append(errs, errors.New("at least one mode is required"))
	}
	for _, p := range c.Exclude {
		if err := validatePattern(p); err != nil {
			errs = append(errs, fmt.Errorf("exclude %q: %v", p, err))
		}
	}
	for _, mode := range sortedKeys(c.Modes) {
		if !NamePattern.MatchString(mode) {
			errs = append(errs, fmt.Errorf("mode name %q must match %s", mode, NamePattern))
		}
		if len(c.Modes[mode]) == 0 {
			// An empty mode would let any change pass the gate.
			errs = append(errs, fmt.Errorf("mode %q has no checks; every mode must require at least one", mode))
		}
		for _, name := range c.Modes[mode] {
			if _, ok := c.Checks[name]; !ok {
				errs = append(errs, fmt.Errorf("mode %q references unknown check %q", mode, name))
			}
		}
	}
	if c.DefaultMode == "" {
		errs = append(errs, errors.New("default_mode is required"))
	} else if _, ok := c.Modes[c.DefaultMode]; !ok {
		errs = append(errs, fmt.Errorf("default_mode %q is not a defined mode", c.DefaultMode))
	}
	return errors.Join(errs...)
}

// NamePattern restricts check and mode names; they become file names.
var NamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validatePattern rejects globs that would silently match nothing.
func validatePattern(p string) error {
	switch {
	case p == "":
		return errors.New("empty pattern")
	case strings.HasPrefix(p, "/") || filepath.IsAbs(p):
		return errors.New("must be relative to the repository root")
	case strings.Contains(p, `\`):
		return errors.New("use forward slashes")
	case p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../"):
		return errors.New("must not leave the repository")
	case strings.ContainsAny(p, "{}[]"):
		return errors.New("braces and brackets are not supported; list each pattern separately")
	case strings.HasSuffix(p, "/"):
		return fmt.Errorf("directories need a wildcard, e.g. %q", p+"**")
	}
	return nil
}

// Fingerprint is the canonical encoding of everything that affects how a check
// runs and what it reads. It salts the inputs hash, so editing the command,
// workdir, inputs or global excludes invalidates existing receipts.
func (c *Config) Fingerprint(name string) string {
	ch := c.Checks[name]
	var requires *Requirements
	if len(ch.Requires.Tools) > 0 || len(ch.Requires.OS) > 0 {
		value := ch.Requires
		value.Tools = append([]string(nil), value.Tools...)
		value.OS = append([]string(nil), value.OS...)
		sort.Strings(value.Tools)
		sort.Strings(value.OS)
		requires = &value
	}
	b, _ := json.Marshal(struct {
		Requires *Requirements `json:"requires,omitempty"`
		Run      string        `json:"run"`
		Workdir  string        `json:"workdir"`
		Inputs   []string      `json:"inputs"`
		Exclude  []string      `json:"exclude"`
	}{requires, ch.Run, ch.Workdir, ch.Inputs, c.Exclude})
	return string(b)
}

// ModeChecks returns the check names required by mode.
func (c *Config) ModeChecks(mode string) ([]string, error) {
	names, ok := c.Modes[mode]
	if !ok {
		return nil, fmt.Errorf("unknown mode %q (defined: %v)", mode, sortedKeys(c.Modes))
	}
	return names, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
