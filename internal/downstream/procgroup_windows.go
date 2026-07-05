//go:build windows

package downstream

import (
	"os/exec"
	"strconv"
)

// configureChildProcess is a no-op on Windows: there's no process-group
// equivalent to set up at spawn time. Cleanup instead happens in killTree.
func configureChildProcess(cmd *exec.Cmd) {}

// killTree force-kills pid and its descendants. On Windows, terminating a
// process does not terminate its children (e.g. `npx` spawning a `node`
// grandchild survives its `cmd.exe` parent's death), so `taskkill /T` is used
// to kill the whole tree explicitly.
func killTree(pid int) {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}
