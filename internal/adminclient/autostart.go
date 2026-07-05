package adminclient

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/shadow-code/shadow-mcp/internal/daemon"
)

// startTimeout must cover a cold daemon start connecting to every configured
// downstream server (e.g. several `npx`-spawned processes resolving/starting
// up) before its admin API comes up - generous on purpose since connects now
// run in parallel (see downstream.NewManager) but a single slow server still
// gates readiness.
const startTimeout = 45 * time.Second

// EnsureRunning returns a Client connected to a running daemon serving
// configPath, auto-spawning `shadow-mcp daemon --config <configPath>` as a
// detached background process if none is currently reachable. Detaching
// (platform-specific, see detach_*.go) lets the daemon outlive whichever
// short-lived stdio adapter or ui process triggered its start.
//
// If two shadow-mcp processes race to start the daemon (e.g. two IDEs
// launching their stdio adapters at once), an exclusive start-lock file
// ensures only one of them actually spawns it; the other waits and attaches
// to the same daemon once it comes up.
func EnsureRunning(configPath string) (*Client, error) {
	if c, ok := tryExisting(); ok {
		return c, nil
	}

	deadline := time.Now().Add(startTimeout)

	release, acquired := acquireStartLock()
	if !acquired {
		// Someone else is already starting it - just wait for them.
		return waitFor(deadline)
	}
	defer release()

	// Re-check now that we hold the lock: the previous holder may have
	// already finished starting the daemon.
	if c, ok := tryExisting(); ok {
		return c, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locating shadow-mcp executable to auto-start the daemon: %w", err)
	}

	cmd := exec.Command(exe, "daemon", "--config", configPath)
	// Auto-started daemons have no console attached, so without this their
	// stdout/stderr - including the health loop's reconnect success/failure
	// logs - would silently vanish, making a stuck downstream connection
	// undiagnosable after the fact. A best-effort failure to open the log
	// file (e.g. DataDir unavailable) shouldn't block the daemon from
	// starting, so it's not treated as fatal.
	if logFile, err := daemonLogFile(); err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		defer logFile.Close() // the child has its own inherited handle
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting shadow-mcp daemon: %w", err)
	}

	return waitFor(deadline)
}

// daemonLogFile opens (truncating) the log file an auto-started daemon's
// stdout/stderr is redirected to.
func daemonLogFile() (*os.File, error) {
	dir, err := daemon.DataDir()
	if err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
}

func waitFor(deadline time.Time) (*Client, error) {
	for time.Now().Before(deadline) {
		if c, ok := tryExisting(); ok {
			return c, nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for shadow-mcp daemon to start")
}

func tryExisting() (*Client, bool) {
	info, err := daemon.ReadInfo()
	if err != nil {
		return nil, false
	}
	c := New(fmt.Sprintf("http://127.0.0.1:%d", info.Port), info.Token)
	if _, err := c.Status(); err != nil {
		return nil, false
	}
	return c, true
}

func acquireStartLock() (release func(), acquired bool) {
	dir, err := daemon.DataDir()
	if err != nil {
		return func() {}, true // best effort: DataDir failing is unusual, don't block startup on it
	}
	lockPath := filepath.Join(dir, "daemon.starting.lock")

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return func() {}, false
	}
	f.Close()

	return func() { os.Remove(lockPath) }, true
}
