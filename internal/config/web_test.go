package config

import (
	"os"
	"strings"
	"testing"
)

func TestWebPack(t *testing.T) {
	root := t.TempDir()
	manifest, err := os.ReadFile("../../packs/web/pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	packFile(t, root, "packs/web/pack.yaml", string(manifest))
	packFile(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: code\npacks: [packs/web/pack.yaml]\nmodes: {code: [web.contracts]}\n")
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Surfaces) != 3 || len(cfg.Checks) != 1 {
		t.Fatalf("unexpected pack exports: %+v", cfg)
	}
	for _, name := range []string{"routes", "dependencies", "env"} {
		s := cfg.Surfaces["web."+name]
		if s.ExtractorVersion != "web-v1" || strings.Join(s.Requires.Tools, ",") != "node,npm,python3" || strings.Join(s.Requires.OS, ",") != "darwin,linux" || len(s.Inputs) == 0 {
			t.Fatalf("surface %s lacks declared contract: %+v", name, s)
		}
		if !strings.Contains(strings.Join(s.Inputs, ","), "examples/web/.env.example") {
			t.Fatalf("surface %s does not fingerprint the explicit env example: %+v", name, s.Inputs)
		}
	}
	c := cfg.Checks["web.contracts"]
	if strings.Join(c.Requires.Tools, ",") != "node,npm,python3" || strings.Join(c.Requires.OS, ",") != "darwin,linux" || len(c.Inputs) == 0 {
		t.Fatalf("build check lacks declared prerequisites: %+v", c)
	}
}
