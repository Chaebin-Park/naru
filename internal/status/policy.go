package status

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/policy"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

func evaluatePolicy(root string, cfg *config.Config, rep *Report) error {
	if !cfg.ModePolicy[rep.Mode] {
		return nil
	}
	rep.Schema = 3
	row := &PolicyStatus{}
	report, err := policy.Evaluate(root)
	switch {
	case err == nil:
		row.State, row.Known, row.New, row.Resolved = report.State, len(report.Known), len(report.New), len(report.Resolved)
		row.InputDigest, row.CandidateDigest, row.BaselineRevision = report.InputDigest, report.CandidateDigest, report.BaselineRevision
	case errors.Is(err, policy.ErrNotReady):
		row.State = "not-ready"
	case errors.Is(err, policy.ErrStale):
		row.State = "stale"
	case errors.Is(err, runlock.ErrBusy):
		row.State = "running"
	default:
		return fmt.Errorf("policy: %w", err)
	}
	if err != nil {
		idx, indexErr := policy.Load(root)
		if indexErr != nil {
			return indexErr
		}
		def := cfg.Surfaces[idx.Surface]
		inputs, hashErr := fileset.Compute(root, def.Inputs, cfg.Exclude, cfg.SurfaceFingerprint(idx.Surface))
		if hashErr != nil {
			return hashErr
		}
		row.InputDigest = strings.TrimPrefix(inputs.Hash, "sha256:")
	}
	switch row.State {
	case "clean", "known", "resolved":
	default:
		rep.Satisfied = false
	}
	rep.Policy = row
	current, err := config.Load(root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(cfg, current) {
		return errors.New("configuration changed during report evaluation; retry status")
	}
	return nil
}
