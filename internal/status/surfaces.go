package status

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/runlock"
)

func evaluateSurfaces(root string, cfg *config.Config, rep *Report) error {
	names := append([]string{}, cfg.ModeSurfaces[rep.Mode]...)
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	// Extended reports use a new version; check-only consumers retain schema 2.
	rep.Schema = 3
	for _, name := range names {
		row := SurfaceStatus{Surface: name}
		diff, err := baseline.Diff(root, name)
		switch {
		case err == nil:
			row.State, row.InputDigest, row.CandidateDigest, row.BaselineRevision = diff.State, diff.InputDigest, diff.NewDigest, diff.BaselineRevision
		case errors.Is(err, baseline.ErrNotReady):
			row.State = "not-ready"
		case errors.Is(err, baseline.ErrStale):
			row.State = "stale"
		case errors.Is(err, runlock.ErrBusy):
			row.State = "running"
		default:
			return fmt.Errorf("surface %q: %w", name, err)
		}
		if err != nil {
			// A blocked row still tracks edits for the Stop progress breaker. This
			// digest never upgrades the failed baseline inspection to a pass.
			def := cfg.Surfaces[name]
			inputs, hashErr := fileset.Compute(root, def.Inputs, cfg.Exclude, cfg.SurfaceFingerprint(name))
			if hashErr != nil {
				return fmt.Errorf("surface %q inputs: %w", name, hashErr)
			}
			row.InputDigest = strings.TrimPrefix(inputs.Hash, "sha256:")
		}
		if row.State != "unchanged" {
			rep.Satisfied = false
		}
		rep.Surfaces = append(rep.Surfaces, row)
	}
	// Diff reloads configuration under its lock. Refuse a mixed-definition report.
	current, err := config.Load(root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(cfg, current) {
		return errors.New("configuration changed during report evaluation; retry status")
	}
	return nil
}
