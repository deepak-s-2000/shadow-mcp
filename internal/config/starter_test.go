package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteStarterCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shadow-mcp.yaml")

	if err := WriteStarter(path); err != nil {
		t.Fatalf("WriteStarter: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the starter config should itself be valid and loadable: %v", err)
	}
	if len(cfg.DownstreamServers) != 0 || len(cfg.Profiles) != 0 || len(cfg.Rules) != 0 {
		t.Errorf("expected an empty starter config, got %+v", cfg)
	}
}

func TestWriteStarterDoesNotClobberExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shadow-mcp.yaml")
	if err := os.WriteFile(path, []byte("# user's own edits\ndownstream_servers: []\n"), 0o600); err != nil {
		t.Fatalf("seeding existing file: %v", err)
	}

	err := WriteStarter(path)
	if err == nil || !os.IsExist(err) {
		t.Fatalf("WriteStarter on an existing file should fail with an already-exists error, got %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "# user's own edits\ndownstream_servers: []\n" {
		t.Errorf("existing file content was modified: %q", string(b))
	}
}
