package config

import (
	"os"
	"strings"
	"testing"
)

func TestSpringBootPack(t *testing.T) {
	root := t.TempDir()
	manifest, err := os.ReadFile("../../packs/spring-boot/pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	packFile(t, root, "packs/spring-boot/pack.yaml", string(manifest))
	packFile(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: code\npacks: [packs/spring-boot/pack.yaml]\nmodes: {code: [spring-boot.contracts]}\n")
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Surfaces) != 4 {
		t.Fatalf("surfaces: %d", len(cfg.Surfaces))
	}
	for _, name := range []string{"api", "routes", "migrations", "config"} {
		surface := cfg.Surfaces["spring-boot."+name]
		if surface.ExtractorVersion != "spring-boot-v1" || strings.Join(surface.Requires.Tools, ",") != "java,mvn,python3" || strings.Join(surface.Requires.OS, ",") != "darwin,linux" {
			t.Fatalf("surface %s: %+v", name, surface)
		}
	}
}
