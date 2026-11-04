//go:build !darwin && !linux && !windows

package runlock

import (
	"errors"
	"os"
)

func lockFile(*os.File) error { return errors.New("execution locking is unsupported on this platform") }
