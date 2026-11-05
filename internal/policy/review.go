package policy

import (
	"context"
	"errors"
	"time"
)

// Preview returns the exact current reduction or initial adoption for human review.
func Preview(root, kind string) (review *Review, retErr error) {
	idx, lock, sl, e := policyLocks(root)
	if e != nil {
		return nil, e
	}
	defer func() { retErr = errors.Join(retErr, sl.Close(), lock.Close()) }()
	_, old, r, e := inspect(lock.Root, idx)
	if e != nil {
		return nil, e
	}
	if e = checkReviewKind(kind, old, r); e != nil {
		return nil, e
	}
	if kind == "reduction" && len(r.Resolved) == 0 {
		r.State = "unchanged"
	}
	return &Review{kind, *r}, nil
}

// Approve rechecks the exact preview under both locks before atomic publication.
// Callers obtain explicit terminal review; this API does not authenticate identity.
func Approve(root string, review Review, reviewer, reason string, ctx context.Context) (record *Record, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	idx, lock, sl, e := policyLocks(root)
	if e != nil {
		return nil, e
	}
	defer func() { retErr = errors.Join(retErr, sl.Close(), lock.Close()) }()
	root = lock.Root
	c, old, r, e := inspect(root, idx)
	if e != nil {
		return nil, e
	}
	if e = checkReviewKind(review.Kind, old, r); e != nil {
		return nil, e
	}
	expected := review.Report
	if expected.CandidateDigest != r.CandidateDigest || expected.DefinitionDigest != r.DefinitionDigest || expected.InputDigest != r.InputDigest || expected.BaselineRevision != r.BaselineRevision {
		return nil, ErrStale
	}
	if review.Kind == "reduction" && len(r.Resolved) == 0 {
		return old, nil
	}
	if !text(reviewer, 128) || !text(reason, 1024) {
		return nil, ErrInvalid
	}
	findings := c.Findings
	var previous *string
	if old != nil {
		previous = &r.BaselineRevision
		// Keep historical presentation for surviving identities, never import new rows.
		current := map[string]bool{}
		for _, f := range c.Findings {
			current[f.ID] = true
		}
		findings = []Finding{}
		for _, f := range old.Findings {
			if current[f.ID] {
				findings = append(findings, f)
			}
		}
	}
	record = &Record{Schema: 1, DefinitionDigest: idx.DefinitionDigest, Findings: findings, Approval: Approval{Kind: review.Kind, PreviousRevision: previous, CandidateDigest: r.CandidateDigest, InputDigest: r.InputDigest, Reviewer: reviewer, Reason: reason, ApprovedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	if e = writeJSON(ctx, root, baselinePath, record); e != nil {
		return nil, e
	}
	return record, nil
}
