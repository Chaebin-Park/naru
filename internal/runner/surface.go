package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Chaebin-Park/naru/internal/baseline"
	"github.com/Chaebin-Park/naru/internal/config"
	"github.com/Chaebin-Park/naru/internal/environment"
	"github.com/Chaebin-Park/naru/internal/fileset"
	"github.com/Chaebin-Park/naru/internal/surface"
)

var ErrSurfaceOutput = errors.New("extractor stdout exceeds 1 MiB")

// Extract publishes a candidate only after successful execution and matching
// before/after inputs. It never writes an approved baseline.
func Extract(root, name string, parent context.Context) (snapshot *surface.Snapshot, retErr error) {
	if parent == nil {
		parent = context.Background()
	}
	if parent.Err() != nil {
		return nil, ErrInterrupted
	}
	lock, err := baseline.Acquire(root, name)
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
	store := baseline.Store{Root: root}
	old, err := store.Current(name)
	if err != nil {
		return nil, err
	}
	if old != nil && (old.Phase == "running" || old.Phase == "recovery") {
		return nil, ErrRecovery
	}
	state := baseline.State{Schema: 1, SurfaceID: name, Phase: "running"}
	if parent.Err() != nil {
		return nil, ErrInterrupted
	}
	if err := store.Put(state); err != nil {
		return nil, err
	}
	fail := func(reason string, cause error) (*surface.Snapshot, error) {
		state.Phase = "failed"
		state.Reason = reason
		state.Snapshot = nil
		if reason == "owner_lost" {
			state.Phase = "recovery"
		}
		return nil, errors.Join(fmt.Errorf("%w: %w", surface.ErrExtraction, cause), store.Put(state))
	}
	if missing := environment.Inspect(def.Requires); missing != nil {
		return fail(missing.Reason, errors.New(missing.Message()))
	}
	if def.Workdir != "" {
		if err := config.ValidateWorkdir(root, def.Workdir); err != nil {
			return fail("workdir_invalid", err)
		}
	}
	before, err := fileset.Compute(root, def.Inputs, cfg.Exclude, cfg.SurfaceFingerprint(name))
	if err != nil {
		return fail("input_error", err)
	}
	if before.FileCount == 0 {
		return fail("empty_inputs", ErrEmptyInputs)
	}
	request := surface.Request{Schema: surface.Schema, SurfaceID: name, ExtractorVersion: def.ExtractorVersion, InputDigest: strings.TrimPrefix(before.Hash, "sha256:")}
	stdin, err := surface.RequestBytes(request)
	if err != nil {
		return fail("request_invalid", err)
	}
	ctx, cancel := context.WithTimeout(parent, def.TimeoutDuration())
	defer cancel()
	stdout := limitedOutput{limit: surface.MaxOutputBytes, cancel: cancel}
	stderr := tailBuffer{}
	cmd := exec.CommandContext(ctx, "sh", "-c", def.Run) // #nosec G204 -- Explicit trusted project extractor command, same shell boundary as checks.
	cmd.Dir = filepath.Join(root, filepath.FromSlash(def.Workdir))
	cmd.Env = append(os.Environ(), "NARU_SURFACE="+name, "NARU_ROOT="+root)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay
	setProcessGroup(cmd)
	runErr := cmd.Run()
	if errors.Is(runErr, exec.ErrWaitDelay) || (runtime.GOOS == "windows" && ctx.Err() != nil) {
		return fail("owner_lost", errors.Join(ErrRecovery, runErr))
	}
	if parent.Err() != nil {
		return fail("cancelled", ErrInterrupted)
	}
	if stdout.exceeded {
		return fail("output_limit", ErrSurfaceOutput)
	}
	if ctx.Err() != nil {
		return fail("timeout", ctx.Err())
	}
	if runErr != nil {
		return fail("extractor_failed", fmt.Errorf("extractor: %w; %s", runErr, stderr.String()))
	}
	snapshot, err = surface.Build(request, stdout.Bytes(), nil)
	if err != nil {
		return fail("output_invalid", err)
	}
	afterCfg, err := config.Load(root)
	if err != nil {
		return fail("definition_unreadable", err)
	}
	afterDef, ok := afterCfg.Surfaces[name]
	if !ok {
		return fail("definition_changed", baseline.ErrStale)
	}
	after, err := fileset.Compute(root, afterDef.Inputs, afterCfg.Exclude, afterCfg.SurfaceFingerprint(name))
	if err != nil {
		return fail("input_error", err)
	}
	if before.Hash != after.Hash || before.FileCount != after.FileCount {
		return fail("input_changed", baseline.ErrStale)
	}
	if parent.Err() != nil {
		return fail("cancelled", ErrInterrupted)
	}
	state.Phase = "ready"
	state.Snapshot = snapshot
	if err := store.Put(state); err != nil {
		return nil, err
	}
	return snapshot, nil
}

type limitedOutput struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		b.exceeded = true
		b.cancel()
		return 0, ErrSurfaceOutput
	}
	return b.buffer.Write(p)
}

func (b *limitedOutput) Bytes() []byte { return b.buffer.Bytes() }
