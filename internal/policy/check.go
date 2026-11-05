package policy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/runlock"
	"github.com/Chaebin-Park/naru/internal/runner"
)

// Check refreshes a complete candidate before comparing with the policy baseline.
func Check(root string, ctx context.Context) (report *Report, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	lock, e := acquirePolicy(root)
	if e != nil {
		return nil, e
	}
	defer func() { retErr = errors.Join(retErr, lock.Close()) }()
	root = lock.Root
	// Invalidate old authority even when subsequent index validation fails.
	if e = writeCandidate(root, candidate{Schema: 1, Phase: "running"}); e != nil {
		return nil, e
	}
	failed := func(cause error) (*Report, error) {
		return nil, errors.Join(cause, writeCandidate(root, candidate{Schema: 1, Phase: "failed", Reason: "check_failed"}))
	}
	idx, e := Load(root)
	if e != nil {
		return failed(e)
	}
	snapshot, e := runner.Extract(root, idx.Surface, ctx)
	if e != nil {
		return failed(e)
	}
	surfaceLock, e := baseline.Acquire(root, idx.Surface)
	if e != nil {
		return failed(e)
	}
	defer func() { retErr = errors.Join(retErr, surfaceLock.Close()) }()
	current, e := Load(root)
	if e != nil {
		return failed(e)
	}
	if current.DefinitionDigest != idx.DefinitionDigest {
		return failed(ErrStale)
	}
	if e = validateSource(root, idx, snapshot.Digest, snapshot.InputDigest); e != nil {
		return failed(e)
	}
	findings, e := ParseFindings(snapshot, idx)
	if e != nil {
		return failed(errors.Join(ErrNotReady, e))
	}
	old, revision, e := loadRecord(root)
	if e != nil {
		return failed(e)
	}
	if e = compatibleRecord(old, idx); e != nil {
		return failed(e)
	}
	if e = ctx.Err(); e != nil {
		return failed(e)
	}
	c := candidate{1, "ready", "", idx.DefinitionDigest, idx.Surface, snapshot.Digest, snapshot.InputDigest, findings}
	if e = writeCandidate(root, c); e != nil {
		return nil, e
	}
	_, digest, e := loadCandidate(root)
	if e != nil {
		return nil, e
	}
	return compare(&c, digest, old, revision), nil
}

func validateSource(root string, idx *Index, snapshotDigest, inputDigest string) error {
	state, e := (baseline.Store{Root: root}).Current(idx.Surface)
	if e != nil {
		return e
	}
	if state == nil || state.Phase != "ready" || state.Snapshot == nil {
		return ErrNotReady
	}
	if state.Snapshot.Digest != snapshotDigest || state.Snapshot.InputDigest != inputDigest {
		return ErrStale
	}
	cfg, e := config.Load(root)
	if e != nil {
		return e
	}
	def, ok := cfg.Surfaces[idx.Surface]
	if !ok {
		return ErrStale
	}
	now, e := fileset.Compute(root, def.Inputs, cfg.Exclude, cfg.SurfaceFingerprint(idx.Surface))
	if e != nil {
		return e
	}
	if now.FileCount == 0 || strings.TrimPrefix(now.Hash, "sha256:") != inputDigest {
		return ErrStale
	}
	return nil
}

// inspect requires the policy and selected surface locks.
func inspect(root string, idx *Index) (*candidate, *Record, *Report, error) {
	c, digest, e := loadCandidate(root)
	if e != nil {
		return nil, nil, nil, e
	}
	if c.DefinitionDigest != idx.DefinitionDigest || c.SurfaceID != idx.Surface {
		return nil, nil, nil, ErrStale
	}
	if e = validateSource(root, idx, c.SnapshotDigest, c.InputDigest); e != nil {
		return nil, nil, nil, e
	}
	// Stored candidates are authority only if their findings match the validated source.
	state, e := (baseline.Store{Root: root}).Current(idx.Surface)
	if e != nil {
		return nil, nil, nil, e
	}
	findings, e := ParseFindings(state.Snapshot, idx)
	if e != nil {
		return nil, nil, nil, e
	}
	if !sameFindings(findings, c.Findings) {
		return nil, nil, nil, ErrInvalid
	}
	old, revision, e := loadRecord(root)
	if e != nil {
		return nil, nil, nil, e
	}
	if e = compatibleRecord(old, idx); e != nil {
		return nil, nil, nil, e
	}
	return c, old, compare(c, digest, old, revision), nil
}
func sameFindings(a, b []Finding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func compare(c *candidate, digest string, old *Record, revision string) *Report {
	r := &Report{State: "clean", DefinitionDigest: c.DefinitionDigest, CandidateDigest: digest, InputDigest: c.InputDigest, BaselineRevision: revision, Known: []Finding{}, New: []Finding{}, Resolved: []Finding{}}
	before := map[string]Finding{}
	if old != nil {
		for _, f := range old.Findings {
			before[f.ID] = f
		}
	}
	seen := map[string]bool{}
	for _, f := range c.Findings {
		seen[f.ID] = true
		if _, ok := before[f.ID]; ok {
			r.Known = append(r.Known, f)
		} else {
			r.New = append(r.New, f)
		}
	}
	for id, f := range before {
		if !seen[id] {
			r.Resolved = append(r.Resolved, f)
		}
	}
	sort.Slice(r.Resolved, func(i, j int) bool { return r.Resolved[i].ID < r.Resolved[j].ID })
	switch {
	case old == nil:
		r.State = "bootstrap-required"
	case len(r.New) > 0:
		r.State = "new"
	case len(r.Resolved) > 0:
		r.State = "resolved"
	case len(r.Known) > 0:
		r.State = "known"
	}
	return r
}

func policyLocks(root string) (*Index, *runlock.Lock, *runlock.Lock, error) {
	lock, e := acquirePolicy(root)
	if e != nil {
		return nil, nil, nil, e
	}
	idx, e := Load(lock.Root)
	if e != nil {
		return nil, nil, nil, errors.Join(e, lock.Close())
	}
	sl, e := baseline.Acquire(lock.Root, idx.Surface)
	if e != nil {
		return nil, nil, nil, errors.Join(e, lock.Close())
	}
	return idx, lock, sl, nil
}
func checkReviewKind(kind string, old *Record, r *Report) error {
	switch kind {
	case "bootstrap":
		if old != nil {
			return fmt.Errorf("%w: bootstrap requires an absent baseline", ErrGrowth)
		}
	case "reduction":
		if old == nil {
			return ErrNotReady
		}
		if len(r.New) > 0 {
			return ErrGrowth
		}
	default:
		return fmt.Errorf("%w: review kind", ErrInvalid)
	}
	return nil
}

func compatibleRecord(record *Record, index *Index) error {
	if record == nil {
		return nil
	}
	if record.DefinitionDigest != index.DefinitionDigest {
		return ErrIncompatible
	}
	for _, finding := range record.Findings {
		if _, ok := index.Rules[finding.RuleID]; !ok {
			return ErrInvalid
		}
	}
	return nil
}

func acquirePolicy(root string) (*runlock.Lock, error) {
	return runlock.AcquireScoped(root, "policy", "state")
}
