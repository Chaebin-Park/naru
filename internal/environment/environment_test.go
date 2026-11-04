package environment

import (
	"github.com/Chaebin-Park/naru/internal/config"
	"os/exec"
	"testing"
)

func TestInspectPrerequisitesWithoutProbes(t *testing.T) {
	for _, tc := range []struct {
		name, platform           string
		req                      config.Requirements
		missing, reason, subject string
		lookups                  int
	}{
		{"implicit shell", "linux", config.Requirements{}, "sh", "missing_tool", "sh", 1},
		{"missing tool", "linux", config.Requirements{Tools: []string{"docker"}}, "docker", "missing_tool", "docker", 2},
		{"unsupported", "windows", config.Requirements{OS: []string{"darwin", "linux"}}, "", "unsupported_platform", "windows", 0},
		{"supported", "linux", config.Requirements{Tools: []string{"go"}, OS: []string{"linux"}}, "", "", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			u := inspect(tc.req, tc.platform, func(name string) (string, error) {
				calls++
				if name == tc.missing {
					return "", exec.ErrDot
				}
				return "/tools/" + name, nil
			})
			if calls != tc.lookups {
				t.Fatalf("lookups %d", calls)
			}
			if tc.reason == "" {
				if u != nil {
					t.Fatal(u)
				}
				return
			}
			if u == nil || u.Reason != tc.reason || u.Subject != tc.subject || !u.Validate() {
				t.Fatalf("%+v", u)
			}
		})
	}
}
