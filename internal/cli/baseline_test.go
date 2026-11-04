package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaselineCLI(t *testing.T) {
	root := newProject(t, false)
	cfg := projectConfig + "surfaces: {api: {run: 'cat output.json', inputs: [output.json], extractor_version: v1}}\n"
	write(t, root, ".naru/config.yaml", cfg)
	write(t, root, "output.json", `{"schema":1,"items":[{"id":"route","value":{"method":"GET"}}]}`)
	if got := run(t, root, "", "baseline", "diff", "api"); got.code != 1 {
		t.Fatalf("no candidate %+v", got)
	}
	extracted := run(t, root, "", "baseline", "extract", "--json", "api")
	if extracted.code != 0 || !strings.Contains(extracted.stdout, `"canonical"`) {
		t.Fatalf("extract %+v", extracted)
	}
	if _, err := os.Stat(filepath.Join(root, ".naru/baselines/api.json")); !os.IsNotExist(err) {
		t.Fatal("extract wrote baseline")
	}
	if got := run(t, root, "", "baseline", "diff", "--json", "api"); got.code != 1 || !strings.Contains(got.stdout, `"state":"missing-baseline"`) {
		t.Fatalf("missing baseline %+v", got)
	}
	write(t, root, ".naru/baselines/api.json", strings.TrimSpace(extracted.stdout))
	if got := run(t, root, "", "baseline", "diff", "api"); got.code != 0 {
		t.Fatalf("unchanged %+v", got)
	}
	write(t, root, "output.json", `{"schema":1,"items":[]}`)
	if got := run(t, root, "", "baseline", "diff", "api"); got.code != 1 {
		t.Fatalf("stale %+v", got)
	}
	if got := run(t, root, "", "baseline", "extract", "api"); got.code != 0 {
		t.Fatalf("empty valid %+v", got)
	}
	if got := run(t, root, "", "baseline", "diff", "--json", "api"); got.code != 1 || !strings.Contains(got.stdout, `"removed":[{"id":"route"`) {
		t.Fatalf("removal %+v", got)
	}
	write(t, root, ".naru/config.yaml", strings.Replace(cfg, "cat output.json", "exit 1", 1))
	if got := run(t, root, "", "baseline", "extract", "api"); got.code != 1 {
		t.Fatalf("failed %+v", got)
	}
	if got := run(t, root, "", "baseline", "diff", "api"); got.code != 1 {
		t.Fatalf("failed candidate reused %+v", got)
	}
	if got := run(t, root, "", "baseline", "extract", "unknown"); got.code != 64 {
		t.Fatalf("unknown %+v", got)
	}
}
