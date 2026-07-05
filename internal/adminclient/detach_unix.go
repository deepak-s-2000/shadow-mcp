//go:build !windows

package adminclient

import (
	"os/exec"
	"syscall"
)

// detach configures cmd so the spawned daemon survives after this process
// exits, instead of being tied to the current session/process group.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
