package surface

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
)

// Exercise this repository's actual test-input declaration so changing a golden
// fixture cannot leave the dogfood gate satisfied by an older test execution.
func TestFixturesAreCheckInputs(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "surface", "testdata")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fixture.json")
	var hashes []string
	for _, value := range []string{`{"schema":1,"items":[]}`, `{"schema":1,"items":[{"id":"x","value":{}}]}`} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		d, err := fileset.Compute(root, cfg.Checks["test"].Inputs, cfg.Exclude, cfg.Fingerprint("test"))
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, d.Hash)
	}
	if hashes[0] == hashes[1] {
		t.Fatal("fixture edit reused the previous test-input digest")
	}
}
