package baseline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/surface"
)

type Item struct {
	ID    string          `json:"id"`
	Value json.RawMessage `json:"value"`
}
type Change struct {
	ID     string          `json:"id"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}
type Report struct {
	SurfaceID         string   `json:"surface_id"`
	State             string   `json:"state"`
	OldDigest         *string  `json:"old_digest"`
	NewDigest         string   `json:"new_digest"`
	DefinitionChanged bool     `json:"definition_changed"`
	Added             []Item   `json:"added"`
	Removed           []Item   `json:"removed"`
	Modified          []Change `json:"modified"`
}

// Diff compares a current verified candidate with a read-only baseline.
func Diff(root, name string) (report *Report, retErr error) {
	lock, err := Acquire(root, name)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, lock.Close()) }()
	root = lock.Root
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	def, ok := cfg.Surfaces[name]
	if !ok {
		return nil, fmt.Errorf("unknown surface %q", name)
	}
	store := Store{Root: root}
	state, err := store.Current(name)
	if err != nil {
		return nil, err
	}
	if state == nil || state.Phase != "ready" {
		return nil, ErrNotReady
	}
	current, err := fileset.Compute(root, def.Inputs, cfg.Exclude, cfg.SurfaceFingerprint(name))
	if err != nil {
		return nil, err
	}
	if current.FileCount == 0 || current.Hash != "sha256:"+state.Snapshot.InputDigest {
		return nil, ErrStale
	}
	approved, err := store.Approved(name)
	if err != nil {
		return nil, err
	}
	return compare(approved, state.Snapshot)
}

func values(s *surface.Snapshot) (map[string]json.RawMessage, error) {
	result := map[string]json.RawMessage{}
	if s == nil {
		return result, nil
	}
	var doc struct {
		Items []Item `json:"items"`
	}
	if err := json.Unmarshal(s.Canonical, &doc); err != nil {
		return nil, err
	}
	for _, item := range doc.Items {
		result[item.ID] = item.Value
	}
	return result, nil
}
func compare(old, next *surface.Snapshot) (*Report, error) {
	before, err := values(old)
	if err != nil {
		return nil, err
	}
	after, err := values(next)
	if err != nil {
		return nil, err
	}
	r := &Report{SurfaceID: next.SurfaceID, State: "missing-baseline", NewDigest: next.Digest, Added: []Item{}, Removed: []Item{}, Modified: []Change{}}
	if old != nil {
		r.OldDigest = &old.Digest
		r.State = "changed"
		r.DefinitionChanged = old.ExtractorVersion != next.ExtractorVersion || old.Policy != next.Policy
		if old.Digest == next.Digest {
			r.State = "unchanged"
		}
	}
	keys := map[string]bool{}
	for id := range before {
		keys[id] = true
	}
	for id := range after {
		keys[id] = true
	}
	names := make([]string, 0, len(keys))
	for id := range keys {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		b, had := before[id]
		a, has := after[id]
		switch {
		case !had:
			r.Added = append(r.Added, Item{id, a})
		case !has:
			r.Removed = append(r.Removed, Item{id, b})
		case !bytes.Equal(b, a):
			r.Modified = append(r.Modified, Change{id, b, a})
		}
	}
	return r, nil
}
