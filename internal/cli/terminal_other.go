//go:build !darwin && !linux

package cli

import "os"

func isTerminal(_ *os.File) bool { return false }
