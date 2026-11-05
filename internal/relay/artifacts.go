package relay

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Chaebin-Park/naru/internal/receipt"
)

func copyArtifacts(ctx context.Context, root, sourceDir string, p receipt.RelayPacket, s receipt.RelaySubmission) (result []receipt.RelayArtifact, retErr error) {
	expected := map[string]bool{}
	for _, name := range p.ExpectedArtifacts {
		expected[name] = true
	}
	if len(s.Artifacts) != len(expected) {
		return nil, fmt.Errorf("%w: artifact set differs from request", ErrInvalid)
	}
	for _, a := range s.Artifacts {
		if !expected[a.Name] {
			return nil, fmt.Errorf("%w: unexpected artifact", ErrInvalid)
		}
		delete(expected, a.Name)
	}
	attempt, err := identity()
	if err != nil {
		return nil, err
	}
	staging := filepath.Join(root, ".naru", "relay", "staging", p.PacketID+"-"+attempt)
	destination := filepath.Join(root, ".naru", "relay", "artifacts", p.PacketID, attempt)
	for _, path := range []string{staging, destination} {
		if err := receipt.RelaySafePath(root, path); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	if err := os.Mkdir(staging, 0o700); err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(staging); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	var total int64
	for _, a := range s.Artifacts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source, err := safeArtifactSource(sourceDir, a.Path)
		if err != nil {
			return nil, err
		}
		data, err := receipt.ReadRelayFile(sourceDir, source, receipt.RelayArtifactLimit)
		if err != nil {
			return nil, fmt.Errorf("%w: artifact read: %v", ErrNotReady, err)
		}
		hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		if hash != a.SHA256 {
			return nil, fmt.Errorf("%w: artifact hash mismatch", ErrNotReady)
		}
		size := int64(len(data))
		total += size
		if total > receipt.RelayTotalLimit {
			return nil, fmt.Errorf("%w: total artifact size", ErrNotReady)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := writeArtifact(filepath.Join(staging, a.Name), data); err != nil {
			return nil, err
		}
		stored := filepath.ToSlash(filepath.Join(".naru", "relay", "artifacts", p.PacketID, attempt, a.Name))
		result = append(result, receipt.RelayArtifact{Name: a.Name, SHA256: hash, Size: size, StoredPath: stored})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := receipt.SyncRelayDirectory(staging); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("artifact attempt already exists")
	}
	if err := os.Rename(staging, destination); err != nil {
		return nil, err
	}
	if err := receipt.SyncRelayDirectory(filepath.Dir(destination)); err != nil {
		return nil, err
	}
	return result, nil
}
func writeArtifact(path string, data []byte) (retErr error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- Private fresh staging directory and validated artifact filename.
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}
