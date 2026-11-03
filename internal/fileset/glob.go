// Package fileset lists repository files and hashes the ones a check depends on.
package fileset

import (
	"regexp"
	"strings"
)

// Matcher matches slash-separated, root-relative paths against glob patterns.
//
// Supported syntax: "*" (any run of non-slash characters), "?" (one non-slash
// character) and "**" (any number of path segments, including none).
// Patterns are anchored at the repository root: "*.go" matches only root files,
// "**/*.go" matches Go files at any depth.
type Matcher struct {
	res []*regexp.Regexp
}

// NewMatcher compiles patterns. An empty pattern list matches nothing.
func NewMatcher(patterns []string) (*Matcher, error) {
	m := &Matcher{}
	for _, p := range patterns {
		re, err := regexp.Compile(globToRegexp(p))
		if err != nil {
			return nil, err
		}
		m.res = append(m.res, re)
	}
	return m, nil
}

// Match reports whether path matches any pattern.
func (m *Matcher) Match(path string) bool {
	for _, re := range m.res {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

func globToRegexp(p string) string {
	p = strings.TrimPrefix(p, "./")
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*' && i+1 < len(p) && p[i+1] == '*':
			// "**/" matches zero or more directories; a bare "**" matches anything.
			if i+2 < len(p) && p[i+2] == '/' {
				b.WriteString("(?:.*/)?")
				i += 2
			} else {
				b.WriteString(".*")
				i++
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}
