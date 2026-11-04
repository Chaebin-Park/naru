// Package environment inspects declared prerequisites without executing commands.
package environment

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Chaebin-Park/naru/internal/config"
)

// Unavailable explains why a command was not attempted.
type Unavailable struct {
	Reason  string `json:"reason"`
	Subject string `json:"subject"`
}

func (u Unavailable) Validate() bool {
	return (u.Reason == "missing_tool" || u.Reason == "unsupported_platform") && u.Subject != "" && len(u.Subject) <= 256 && !strings.ContainsAny(u.Subject, "\x00\r\n")
}
func (u Unavailable) Message() string {
	if u.Reason == "unsupported_platform" {
		return fmt.Sprintf("platform %s is unsupported; rerun this check on a declared supported OS", u.Subject)
	}
	return fmt.Sprintf("required executable %q is unavailable on PATH; provide it or use a configured CI environment, then rerun this check", u.Subject)
}

// Inspect does not run version probes, contact services, or install tools.
func Inspect(r config.Requirements) *Unavailable { return inspect(r, runtime.GOOS, exec.LookPath) }
func inspect(r config.Requirements, platform string, lookup func(string) (string, error)) *Unavailable {
	if len(r.OS) > 0 {
		allowed := false
		for _, candidate := range r.OS {
			if candidate == platform {
				allowed = true
			}
		}
		if !allowed {
			return &Unavailable{Reason: "unsupported_platform", Subject: platform}
		}
	}
	for _, tool := range append([]string{"sh"}, r.Tools...) {
		if _, err := lookup(tool); err != nil {
			return &Unavailable{Reason: "missing_tool", Subject: tool}
		}
	}
	return nil
}
