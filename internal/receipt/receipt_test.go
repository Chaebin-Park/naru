package receipt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailedReplacementRemovesTemporaryReceipt(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	if err := os.MkdirAll(filepath.Join(s.dir, "check.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(Receipt{Check: "check", Schema: Schema}); err == nil {
		t.Fatal("replacement unexpectedly succeeded")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "check.json" {
		t.Fatalf("temporary receipt leaked: %v", entries)
	}
}
