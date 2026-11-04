package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// safeRelative inspects each component; symlinked roots are canonicalized but
// paths beneath that root cannot redirect outside it. This is not a sandbox
// against a hostile process racing filesystem changes.
func safeRelative(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsAny(rel, `\:`) || filepath.ToSlash(filepath.Clean(rel)) != rel {
		return "", errors.New("path must be clean and repository-relative")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path := root
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || part == "." || part == "" {
			return "", errors.New("invalid path component")
		}
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlinks are not allowed in pack paths")
		}
	}
	return path, nil
}

func packWorkdir(root, rel string) error {
	if rel == "." {
		return nil
	}
	path, err := safeRelative(root, rel)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("pack workdir must be a directory")
	}
	return nil
}

func readPack(root, rel string) ([]byte, string, error) {
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || parts[0] != "packs" || !NamePattern.MatchString(parts[1]) || parts[2] != "pack.yaml" {
		return nil, "", errors.New("use packs/<directory>/pack.yaml")
	}
	path, err := safeRelative(root, rel)
	if err != nil {
		return nil, "", err
	}
	base := filepath.Dir(path)
	type entry struct {
		Path, Hash string
		Executable bool
	}
	entries := []entry{}
	remaining := int64(16 << 20)
	var manifest []byte
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("pack contains a symlink")
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || len(entries) >= 256 {
			return errors.New("pack needs at most 256 regular files")
		}
		b, err := readBounded(path, remaining)
		if err != nil {
			return err
		}
		remaining -= int64(len(b))
		name, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if name == "pack.yaml" {
			manifest = b
		}
		sum := sha256.Sum256(b)
		entries = append(entries, entry{name, hex.EncodeToString(sum[:]), info.Mode()&0o111 != 0})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if len(manifest) == 0 || len(manifest) > 1<<20 {
		return nil, "", errors.New("manifest must contain at most 1 MiB")
	}
	encoded, err := json.Marshal(struct {
		Path  string
		Files []entry
	}{rel, entries})
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(encoded)
	return manifest, hex.EncodeToString(sum[:]), nil
}

func readBounded(path string, limit int64) (data []byte, retErr error) {
	f, err := os.Open(path) // #nosec G304 -- Selected pack files were checked beneath the project root; trusted local filesystem boundary.
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("pack exceeds 16 MiB total")
	}
	return data, nil
}
