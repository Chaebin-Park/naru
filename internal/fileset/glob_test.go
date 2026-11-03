package fileset

import "testing"

func TestMatcher(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "cmd/main.go", false}, // anchored at root
		{"**/*.go", "main.go", true},   // "**/" matches zero directories
		{"**/*.go", "a/b/c.go", true},
		{"**/*.go", "a/b/c.gox", false},
		{"src/**", "src/a/b.txt", true},
		{"src/**", "srcx/a.txt", false},
		{"go.mod", "go.mod", true},
		{"go.mod", "sub/go.mod", false},
		{"a?.txt", "ab.txt", true},
		{"a?.txt", "a/.txt", false},
		{"./docs/*.md", "docs/x.md", true},
		{"**", "anything/at/all", true},
		{"file.name+[x]", "file.name+[x]", true}, // regexp metacharacters are literal
	}
	for _, c := range cases {
		m, err := NewMatcher([]string{c.pattern})
		if err != nil {
			t.Fatalf("NewMatcher(%q): %v", c.pattern, err)
		}
		if got := m.Match(c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestEmptyMatcherMatchesNothing(t *testing.T) {
	m, _ := NewMatcher(nil)
	if m.Match("a.go") {
		t.Fatal("empty matcher matched")
	}
}
