package config

import (
	"os"
	"strings"
	"testing"
)

func TestAndroidPack(t *testing.T) {
	root := t.TempDir()
	manifest, err := os.ReadFile("../../packs/android/pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	packFile(t, root, "packs/android/pack.yaml", string(manifest))
	packFile(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: code\npacks: [packs/android/pack.yaml]\nmodes: {code: [android.contracts]}\n")
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Surfaces) != 2 || len(cfg.Checks) != 1 {
		t.Fatalf("unexpected pack exports: %+v", cfg)
	}
	for _, name := range []string{"manifest", "dependencies"} {
		s := cfg.Surfaces["android."+name]
		if s.ExtractorVersion != "android-v1" || strings.Join(s.Requires.Tools, ",") != "gradle,java,python3,sdkmanager" || strings.Join(s.Requires.OS, ",") != "darwin,linux" || len(s.Inputs) == 0 {
			t.Fatalf("surface %s lacks declared contract: %+v", name, s)
		}
	}
	c := cfg.Checks["android.contracts"]
	if strings.Join(c.Requires.Tools, ",") != "gradle,java,python3,sdkmanager" || strings.Join(c.Requires.OS, ",") != "darwin,linux" || len(c.Inputs) == 0 {
		t.Fatalf("build check lacks declared prerequisites: %+v", c)
	}
}
