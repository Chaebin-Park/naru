package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Chaebin-Park/naru/internal/config"
	"go.yaml.in/yaml/v3"
)

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func text(s string, n int) bool {
	return s != "" && len(s) <= n && strings.TrimSpace(s) == s && strings.IndexFunc(s, unicode.IsControl) < 0
}
func relative(s string) bool {
	return s != "" && !filepath.IsAbs(s) && !strings.ContainsAny(s, `\:`) && filepath.ToSlash(filepath.Clean(s)) == s && s != "." && s != ".." && !strings.HasPrefix(s, "../")
}

// safePath rejects symlinks at every path component. It is not a hostile-process sandbox.
func safePath(root, rel string, missing bool) (string, error) {
	if !relative(rel) {
		return "", fmt.Errorf("%w: invalid relative path %q", ErrInvalid, rel)
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	p := root
	for _, part := range strings.Split(rel, "/") {
		p = filepath.Join(p, part)
		info, e := os.Lstat(p) // #nosec G703 -- Root-relative policy path components are validated before access.
		if missing && errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return "", fmt.Errorf("%w: non-regular path %s", ErrInvalid, rel)
		}
	}
	return p, nil
}
func readBounded(path string, limit int64) (data []byte, retErr error) {
	f, err := os.Open(path) // #nosec G304 -- Callers validate repository-relative policy or internal state paths.
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalid
	}
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrInvalid
	}
	return data, nil
}
func validateYAML(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Tag == "!!null" {
		return ErrInvalid
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
				return ErrInvalid
			}
			seen[k.Value] = true
		}
	}
	for _, c := range n.Content {
		if err := validateYAML(c); err != nil {
			return err
		}
	}
	return nil
}

// Load validates the index, references and selected surface and binds definition bytes.
func Load(root string) (*Index, error) {
	path, err := safePath(root, ".naru/policy.yml", false)
	if err != nil {
		return nil, fmt.Errorf("%w: index: %w", ErrInvalid, err)
	}
	data, err := readBounded(path, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err = validateYAML(&node); err != nil {
		return nil, err
	}
	var idx Index
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err = dec.Decode(&idx); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	var extra any
	if err = dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}
	cfg, err := config.Load(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if idx.Version != 1 || len(idx.Rules) == 0 || len(idx.Rules) > 128 || len(idx.Sources) == 0 || len(idx.DefinitionFiles) == 0 {
		return nil, ErrInvalid
	}
	if _, ok := cfg.Surfaces[idx.Surface]; !ok {
		return nil, fmt.Errorf("%w: unknown surface %s", ErrInvalid, idx.Surface)
	}
	files := map[string]bool{}
	for _, p := range idx.DefinitionFiles {
		if files[p] {
			return nil, ErrInvalid
		}
		files[p] = true
	}
	for id, p := range idx.Sources {
		if !config.NamePattern.MatchString(id) || len(id) > 128 {
			return nil, ErrInvalid
		}
		files[p] = true
	}
	fixtures := 0
	for id, r := range idx.Rules {
		if !config.NamePattern.MatchString(id) || len(id) > 64 || !text(r.Tool, 128) || len(r.Fixtures) < 2 {
			return nil, ErrInvalid
		}
		if _, ok := idx.Sources[r.Source]; !ok {
			return nil, fmt.Errorf("%w: rule %s has unknown source", ErrInvalid, id)
		}
		files[r.ToolConfig] = true
		expects := map[string]bool{}
		projects := map[string]bool{}
		for _, f := range r.Fixtures {
			fixtures++
			if fixtures > 256 || (f.Expect != "pass" && f.Expect != "violation") || projects[f.Project] {
				return nil, ErrInvalid
			}
			expects[f.Expect] = true
			projects[f.Project] = true
			p, e := safePath(root, f.Project, false)
			if e != nil {
				return nil, fmt.Errorf("%w: fixture: %w", ErrInvalid, e)
			}
			info, e := os.Stat(p)
			if e != nil || !info.IsDir() {
				return nil, ErrInvalid
			}
		}
		if !expects["pass"] || !expects["violation"] {
			return nil, ErrInvalid
		}
	}
	if len(files) > 256 {
		return nil, ErrInvalid
	}
	type entry struct {
		Path, Hash string
		Executable bool
	}
	entries := []entry{}
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	remaining := int64(16 << 20)
	for _, rel := range names {
		p, e := safePath(root, rel, false)
		if e != nil {
			return nil, fmt.Errorf("%w: definition: %w", ErrInvalid, e)
		}
		b, e := readBounded(p, 1<<20)
		if e != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalid, e)
		}
		remaining -= int64(len(b))
		if remaining < 0 {
			return nil, ErrInvalid
		}
		info, e := os.Stat(p)
		if e != nil {
			return nil, e
		}
		entries = append(entries, entry{rel, hash(b), info.Mode()&0o111 != 0})
	}
	encoded, err := json.Marshal(struct {
		Version int
		Index   *Index
		Surface string
		Exclude []string
		Files   []entry
	}{1, &idx, cfg.SurfaceFingerprint(idx.Surface), cfg.Exclude, entries})
	if err != nil {
		return nil, err
	}
	idx.DefinitionDigest = hash(encoded)
	return &idx, nil
}
