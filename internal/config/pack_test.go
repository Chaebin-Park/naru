package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const packConfig = `version: 1
default_mode: code
packs: [packs/demo/pack.yaml]
modes: {code: [demo.test]}
`
const testPack = `schema: 1
id: demo
version: 1.0.0
requires: {tools: [git], os: [darwin, linux]}
supported_tools: {git: '2.x (fixture only)'}
checks:
  test:
    run: 'true'
    inputs: [in.txt]
    requires: {tools: [sh], os: [linux]}
surfaces:
  routes:
    run: 'printf ''{"schema":1,"items":[]}'''
    inputs: [in.txt]
    extractor_version: routes-v1
`

func packFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	} // #nosec G703 -- Test-owned root and fixture-relative path, no external input.
}
func packProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	packFile(t, root, ".naru/config.yaml", packConfig)
	packFile(t, root, "packs/demo/pack.yaml", testPack)
	return root
}
func TestPackLoading(t *testing.T) {
	root := packProject(t)
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Checks["demo.test"].Requires; strings.Join(got.Tools, ",") != "git,sh" || strings.Join(got.OS, ",") != "linux" {
		t.Fatalf("requires %+v", got)
	}
	if c.Surfaces["demo.routes"].ExtractorVersion != "routes-v1" || len(c.Surfaces["demo.routes"].Requires.OS) != 2 {
		t.Fatal("surface missing inherited contract")
	}
	before := c.Fingerprint("demo.test")
	surfaceBefore := c.SurfaceFingerprint("demo.routes")
	packFile(t, root, "packs/demo/helper.sh", "echo version1\n")
	next, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == next.Fingerprint("demo.test") || surfaceBefore == next.SurfaceFingerprint("demo.routes") {
		t.Fatal("bundled asset omitted from fingerprint")
	}
	if _, err := Parse([]byte(packConfig)); err == nil {
		t.Fatal("rootless parse resolved a pack")
	}
}
func TestPackRejections(t *testing.T) {
	for name, change := range map[string]func(*testing.T, string){
		"schema": func(t *testing.T, r string) {
			packFile(t, r, "packs/demo/pack.yaml", strings.Replace(testPack, "schema: 1", "schema: 2", 1))
		},
		"requirements": func(t *testing.T, r string) {
			packFile(t, r, "packs/demo/pack.yaml", strings.Replace(testPack, "[git]", "[../git]", 1))
		},
		"disjoint OS": func(t *testing.T, r string) {
			packFile(t, r, "packs/demo/pack.yaml", strings.Replace(testPack, "os: [linux]", "os: [windows]", 1))
		},
		"duplicate pack": func(t *testing.T, r string) {
			packFile(t, r, ".naru/config.yaml", strings.Replace(packConfig, "[packs/demo/pack.yaml]", "[packs/demo/pack.yaml, packs/demo/pack.yaml]", 1))
		},
		"check collision": func(t *testing.T, r string) {
			packFile(t, r, ".naru/config.yaml", packConfig+"checks: {demo.test: {run: 'true', inputs: [in.txt]}}\n")
		},
		"surface collision": func(t *testing.T, r string) {
			packFile(t, r, ".naru/config.yaml", packConfig+"surfaces: {demo.routes: {run: 'true', inputs: [in.txt], extractor_version: v1}}\n")
		},
		"traversal": func(t *testing.T, r string) {
			packFile(t, r, ".naru/config.yaml", strings.Replace(packConfig, "packs/demo/pack.yaml", "packs/../pack.yaml", 1))
		},
		"absolute": func(t *testing.T, r string) {
			packFile(t, r, ".naru/config.yaml", strings.Replace(packConfig, "packs/demo/pack.yaml", "/tmp/pack.yaml", 1))
		},
		"workdir traversal": func(t *testing.T, r string) {
			packFile(t, r, "packs/demo/pack.yaml", strings.Replace(testPack, "run: 'true'", "run: 'true'\n    workdir: ../outside", 1))
		},
		"unknown field":      func(t *testing.T, r string) { packFile(t, r, "packs/demo/pack.yaml", testPack+"unknown: true\n") },
		"multiple documents": func(t *testing.T, r string) { packFile(t, r, "packs/demo/pack.yaml", testPack+"---\nschema: 1\n") },
		"duplicate YAML key": func(t *testing.T, r string) { packFile(t, r, "packs/demo/pack.yaml", testPack+"id: another\n") },
		"symlink asset": func(t *testing.T, r string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(r, "packs/demo/escape")); err != nil {
				t.Fatal(err)
			}
		},
		"symlink manifest": func(t *testing.T, r string) {
			p := filepath.Join(r, "packs/demo/pack.yaml")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			packFile(t, outside, "pack.yaml", testPack)
			if err := os.Symlink(filepath.Join(outside, "pack.yaml"), p); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := packProject(t)
			change(t, root)
			if _, err := Load(root); err == nil {
				t.Fatal("invalid pack accepted")
			}
		})
	}
}
