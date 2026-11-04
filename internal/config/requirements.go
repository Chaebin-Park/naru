package config

import (
	"fmt"
	"regexp"
)

// Requirements declares platform and executable availability, not service health.
type Requirements struct {
	Tools []string `yaml:"tools" json:"tools,omitempty"`
	OS    []string `yaml:"os" json:"os,omitempty"`
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

func (r Requirements) validate() error {
	seen := map[string]bool{}
	for _, tool := range r.Tools {
		if len(tool) > 256 || !toolName.MatchString(tool) || seen[tool] {
			return fmt.Errorf("requires.tools: invalid or duplicate executable name %q", tool)
		}
		seen[tool] = true
	}
	seen = map[string]bool{}
	for _, platform := range r.OS {
		if (platform != "darwin" && platform != "linux" && platform != "windows") || seen[platform] {
			return fmt.Errorf("requires.os: unsupported or duplicate platform %q (use darwin, linux, windows)", platform)
		}
		seen[platform] = true
	}
	return nil
}
