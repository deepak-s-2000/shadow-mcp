package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Info is what a running daemon publishes so other shadow-mcp processes
// (the stdio adapter, `shadow-mcp ui`) can find and authenticate to it.
type Info struct {
	PID   int    `json:"pid"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// DataDir returns (creating if needed) the per-user directory shadow-mcp uses
// for its daemon info file.
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "shadow-mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func infoPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.json"), nil
}

// usagePath returns the path to the persisted per-profile tool usage counts
// (see internal/usage), alongside the daemon info file in the same data dir.
func usagePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "usage.json"), nil
}

// WriteInfo persists a running daemon's connection info for other processes to find.
func WriteInfo(info Info) error {
	path, err := infoPath()
	if err != nil {
		return err
	}
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// ReadInfo reads the currently published daemon info, if any.
func ReadInfo() (Info, error) {
	var info Info
	path, err := infoPath()
	if err != nil {
		return info, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(b, &info)
	return info, err
}

// RemoveInfo deletes the published daemon info, e.g. on clean shutdown.
func RemoveInfo() error {
	path, err := infoPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// GenerateToken returns a fresh random per-run admin API token.
func GenerateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
