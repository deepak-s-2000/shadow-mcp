//go:build !windows

package downstream

import (
	"os/exec"
	"syscall"
)

// configureChildProcess puts the child in its own process group at spawn
// time, so killTree can signal the whole group without affecting shadow-mcp
// itself.
func configureChildProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree force-kills every process in pid's process group (see
// configureChildProcess).
func killTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
