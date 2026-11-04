package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Surface declares an extractor using the surface-json-v1 stdout protocol.
// It is not a check and cannot satisfy a mode by itself.
type Surface struct {
	Check            `yaml:",inline"`
	ExtractorVersion string `yaml:"extractor_version"`
}

type manifest struct {
	Schema         int                `yaml:"schema"`
	ID             string             `yaml:"id"`
	Version        string             `yaml:"version"`
	Requires       Requirements       `yaml:"requires"`
	SupportedTools map[string]string  `yaml:"supported_tools"`
	Checks         map[string]Check   `yaml:"checks"`
	Surfaces       map[string]Surface `yaml:"surfaces"`
}

func (c *Config) SurfaceFingerprint(name string) string {
	s := c.Surfaces[name]
	b, _ := json.Marshal(struct {
		Definition       string
		ExtractorVersion string
	}{c.fingerprint(s.Check), s.ExtractorVersion})
	return string(b)
}

var extractorVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+\-]{0,127}$`)

func validateSurface(name string, s Surface) error {
	if len(name) > 128 || !extractorVersion.MatchString(s.ExtractorVersion) {
		return fmt.Errorf("surface %q: invalid extractor_version", name)
	}
	// Use exactly the same command, inputs, timeout and requires contract as checks.
	temp := Config{Version: 1, DefaultMode: "surface", Modes: map[string][]string{"surface": {name}}, Checks: map[string]Check{name: s.Check}}
	return temp.Validate()
}

func (c *Config) loadPacks(root string) error {
	seen := map[string]bool{}
	if c.Checks == nil {
		c.Checks = map[string]Check{}
	}
	if c.Surfaces == nil {
		c.Surfaces = map[string]Surface{}
	}
	for _, path := range c.Packs {
		data, digest, err := readPack(root, path)
		if err != nil {
			return fmt.Errorf("pack %q: %w", path, err)
		}
		var m manifest
		if err := decodeYAML(data, &m); err != nil {
			return fmt.Errorf("pack %q: %w", path, err)
		}
		if m.Schema != 1 || !NamePattern.MatchString(m.ID) || len(m.ID) > 64 || !NamePattern.MatchString(m.Version) || len(m.Version) > 64 {
			return fmt.Errorf("pack %q: invalid schema, id or version", path)
		}
		if seen[m.ID] {
			return fmt.Errorf("duplicate pack id %q", m.ID)
		}
		seen[m.ID] = true
		if len(m.Checks)+len(m.Surfaces) == 0 {
			return fmt.Errorf("pack %q is empty", m.ID)
		}
		if err := m.Requires.validate(); err != nil {
			return err
		}
		for tool, version := range m.SupportedTools {
			if err := (Requirements{Tools: []string{tool}}).validate(); err != nil {
				return err
			}
			if strings.TrimSpace(version) == "" || len(version) > 256 {
				return errors.New("supported_tools requires a nonempty version description")
			}
		}
		prepare := func(local string, ch Check) (string, Check, error) {
			if !NamePattern.MatchString(local) || len(local) > 64 {
				return "", ch, errors.New("invalid pack entry name")
			}
			name := m.ID + "." + local
			if len(name) > 128 {
				return "", ch, errors.New("namespaced entry exceeds 128 bytes")
			}
			ch.Requires, err = mergeRequirements(m.Requires, ch.Requires)
			if err != nil {
				return "", ch, err
			}
			if ch.Workdir != "" {
				if err := packWorkdir(root, ch.Workdir); err != nil {
					return "", ch, err
				}
			}
			ch.packDigest = digest
			return name, ch, nil
		}
		for _, local := range sortedKeys(m.Checks) {
			name, ch, err := prepare(local, m.Checks[local])
			if err != nil {
				return fmt.Errorf("pack %q check %q: %w", m.ID, local, err)
			}
			if _, exists := c.Checks[name]; exists {
				return fmt.Errorf("duplicate check %q", name)
			}
			c.Checks[name] = ch
		}
		for _, local := range sortedKeys(m.Surfaces) {
			s := m.Surfaces[local]
			name, ch, err := prepare(local, s.Check)
			if err != nil {
				return fmt.Errorf("pack %q surface %q: %w", m.ID, local, err)
			}
			if _, exists := c.Surfaces[name]; exists {
				return fmt.Errorf("duplicate surface %q", name)
			}
			s.Check = ch
			c.Surfaces[name] = s
		}
	}
	return nil
}

// Tools are a union, while allowed platforms are an intersection. A child
// cannot silently widen the pack's platform restriction.
func mergeRequirements(parent, child Requirements) (Requirements, error) {
	if err := child.validate(); err != nil {
		return Requirements{}, err
	}
	r := Requirements{}
	tools := map[string]bool{}
	for _, tool := range append(append([]string{}, parent.Tools...), child.Tools...) {
		tools[tool] = true
	}
	for tool := range tools {
		r.Tools = append(r.Tools, tool)
	}
	sort.Strings(r.Tools)
	switch {
	case len(parent.OS) == 0:
		r.OS = append([]string{}, child.OS...)
	case len(child.OS) == 0:
		r.OS = append([]string{}, parent.OS...)
	default:
		for _, a := range parent.OS {
			for _, b := range child.OS {
				if a == b {
					r.OS = append(r.OS, a)
				}
			}
		}
		if len(r.OS) == 0 {
			return r, errors.New("pack and entry have no supported OS in common")
		}
	}
	sort.Strings(r.OS)
	return r, nil
}
