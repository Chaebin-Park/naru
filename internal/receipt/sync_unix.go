//go:build unix

package receipt

import (
	"errors"
	"os"
)

func syncDirectory(path string) error {
	f, err := os.Open(path) // #nosec G304 -- Directory selected by the internal state store.
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
