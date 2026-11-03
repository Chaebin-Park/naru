//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// setProcessGroup runs the check in its own process group so a timeout kills
// the whole tree (e.g. `sh -c "gradle test"` and its JVMs), not just `sh`.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
