package cli

import (
	"os"
	"strings"
	"testing"
)

func TestRepositoryPackCLI(t *testing.T) {
	root := newProject(t, false)
	data, err := os.ReadFile("../config/testdata/project/packs/dummy/pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "packs/dummy/pack.yaml", string(data))
	write(t, root, "README.md", "public sample\n")
	write(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: code\npacks: [packs/dummy/pack.yaml]\nmodes: {code: [dummy.readme]}\n")
	if got := run(t, root, "", "check"); got.code != 0 {
		t.Fatalf("check %+v", got)
	}
	if got := run(t, root, "", "status", "--json"); got.code != 0 || !strings.Contains(got.stdout, `"check": "dummy.readme"`) {
		t.Fatalf("status %+v", got)
	}
	write(t, root, "packs/dummy/pack.yaml", strings.Replace(string(data), "version: 1.0.0", "version: 1.0.1", 1))
	if got := run(t, root, "", "status"); got.code != 1 {
		t.Fatalf("changed pack reused pass %+v", got)
	}
	if got := run(t, root, "", "gate", "pre-push"); got.code != 1 {
		t.Fatalf("changed pack bypassed pre-push %+v", got)
	}
}
