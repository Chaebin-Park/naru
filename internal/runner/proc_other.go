//go:build !unix

package runner

import "os/exec"

// setProcessGroup is a no-op on non-unix platforms; the default cancel kills the shell.
func setProcessGroup(cmd *exec.Cmd) {}

// Non-Unix cancellation keeps the existing conservative platform policy.
func processGroupUnresolved(cmd *exec.Cmd) bool { return false }
