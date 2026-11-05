//go:build unix

package runner

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup directs cancellation to the command's process group, not
// only its shell. A concurrent fork may survive the initial group signal.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// processGroupUnresolved checks after Run whether the original group is gone.
// It never sends another terminating signal: Wait may already have reaped the
// leader, allowing its numeric ID to be reused by an unrelated process group.
func processGroupUnresolved(cmd *exec.Cmd) bool {
	if cmd.Process == nil {
		return false
	}
	return waitProcessGroup(cmd.Process.Pid, syscall.Kill, 100*time.Millisecond)
}

// waitProcessGroup allows recently killed members to be reaped. Only ESRCH
// confirms absence; permission errors and other uncertainty require recovery.
func waitProcessGroup(pid int, probe func(int, syscall.Signal) error, delay time.Duration) bool {
	if pid <= 1 {
		return true
	}
	deadline := time.Now().Add(delay)
	for {
		err := probe(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return false
		}
		remaining := time.Until(deadline)
		if err != nil || remaining <= 0 {
			return true
		}
		time.Sleep(min(5*time.Millisecond, remaining))
	}
}
