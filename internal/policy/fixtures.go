package policy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/runner"
)

// TestFixtures executes bounded fixture copies, never writing into source fixtures.
func TestFixtures(root, rule string, ctx context.Context) (*FixtureReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	index, err := Load(root)
	if err != nil {
		return nil, err
	}
	parent, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(index.Rules))
	if rule != "" {
		if _, ok := index.Rules[rule]; !ok {
			return nil, fmt.Errorf("%w: unknown rule %q", ErrInvalid, rule)
		}
		names = append(names, rule)
	} else {
		for name := range index.Rules {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	report := &FixtureReport{Passed: true}
	for _, name := range names {
		for _, fixture := range index.Rules[name].Fixtures {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result := FixtureResult{RuleID: name, Project: fixture.Project, Expect: fixture.Expect}
			if err := testFixture(root, index, parent, name, fixture, ctx); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				result.Error = err.Error()
				report.Passed = false
			} else {
				result.Passed = true
			}
			report.Results = append(report.Results, result)
		}
	}
	after, err := Load(root)
	if err != nil {
		return nil, err
	}
	if after.DefinitionDigest != index.DefinitionDigest {
		return nil, ErrStale
	}
	return report, nil
}

func testFixture(root string, index *Index, parent *config.Config, rule string, fixture Fixture, ctx context.Context) (retErr error) {
	temp, err := os.MkdirTemp("", "naru-policy-fixture-")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStorage, err)
	}
	defer func() { retErr = errors.Join(retErr, os.RemoveAll(temp)) }()
	if err := copyFixture(filepath.Join(root, filepath.FromSlash(fixture.Project)), temp); err != nil {
		return err
	}
	cfg, err := config.Load(temp)
	if err != nil {
		return err
	}
	if _, ok := cfg.Surfaces[index.Surface]; !ok {
		return fmt.Errorf("%w: fixture surface missing", ErrInvalid)
	}
	if cfg.SurfaceFingerprint(index.Surface) != parent.SurfaceFingerprint(index.Surface) || !reflect.DeepEqual(cfg.Exclude, parent.Exclude) {
		return fmt.Errorf("%w: fixture surface definition differs", ErrInvalid)
	}
	definitions := append([]string(nil), index.DefinitionFiles...)
	for _, r := range index.Rules {
		definitions = append(definitions, r.ToolConfig)
	}
	for _, path := range definitions {
		source := filepath.Join(root, filepath.FromSlash(path))
		copyPath := filepath.Join(temp, filepath.FromSlash(path))
		a, err := fixtureFile(source)
		if err != nil {
			return err
		}
		b, err := fixtureFile(copyPath)
		if err != nil {
			return err
		}
		ai, err := os.Lstat(source)
		if err != nil {
			return err
		}
		bi, err := os.Lstat(copyPath)
		if err != nil {
			return err
		}
		if string(a) != string(b) || ai.Mode().Perm()&0o111 != bi.Mode().Perm()&0o111 {
			return fmt.Errorf("%w: fixture definition differs: %s", ErrInvalid, path)
		}
	}
	snapshot, err := runner.Extract(temp, index.Surface, ctx)
	if err != nil {
		return err
	}
	findings, err := ParseFindings(snapshot, index)
	if err != nil {
		return err
	}
	count := 0
	for _, finding := range findings {
		if finding.RuleID == rule {
			count++
		}
	}
	if (fixture.Expect == "pass" && count != 0) || (fixture.Expect == "violation" && count == 0) {
		return fmt.Errorf("fixture expected %s, got %d findings for %s", fixture.Expect, count, rule)
	}
	return nil
}

func fixtureFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, fmt.Errorf("%w: unsafe or oversized fixture file", ErrInvalid)
	}
	f, err := os.Open(path) // #nosec G304 -- Paths are validated repository-relative definitions or bounded temporary fixture copies.
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("%w: fixture file exceeds bound", ErrInvalid)
	}
	return data, nil
}

func copyFixture(source, target string) error {
	count, total := 0, 0
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: fixture symlink %s", ErrInvalid, slash)
		}
		if slash == ".git" || strings.HasPrefix(slash, ".git/") || slash == ".naru/state" || strings.HasPrefix(slash, ".naru/state/") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		data, err := fixtureFile(path)
		if err != nil {
			return err
		}
		count++
		total += len(data)
		if count > 1024 || total > 16<<20 {
			return fmt.Errorf("%w: fixture copy exceeds bounds", ErrInvalid)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600|(info.Mode().Perm()&0o111)) // #nosec G703 -- Walk-relative path in a freshly created private temporary directory; symlinks rejected.
	})
}
