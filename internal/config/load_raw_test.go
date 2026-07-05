package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRawDoesNotInterpolate(t *testing.T) {
	t.Setenv("SHADOW_MCP_TEST_TOKEN", "super-secret-value")

	path := filepath.Join(t.TempDir(), "shadow-mcp.yaml")
	body := `downstream_servers:
  - name: github
    transport: stdio
    command: npx
    env:
      GITHUB_TOKEN: "${SHADOW_MCP_TEST_TOKEN}"
profiles: []
rules: []
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	raw, err := LoadRaw(path)
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	if got := raw.DownstreamServers[0].Env["GITHUB_TOKEN"]; got != "${SHADOW_MCP_TEST_TOKEN}" {
		t.Fatalf("LoadRaw resolved the placeholder: got %q, want the literal ${SHADOW_MCP_TEST_TOKEN}", got)
	}

	interpolated, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := interpolated.DownstreamServers[0].Env["GITHUB_TOKEN"]; got != "super-secret-value" {
		t.Fatalf("Load should still interpolate: got %q", got)
	}
}

func TestSaveThenLoadRawNeverBakesInSecret(t *testing.T) {
	t.Setenv("SHADOW_MCP_TEST_TOKEN", "super-secret-value")

	path := filepath.Join(t.TempDir(), "shadow-mcp.yaml")
	body := `downstream_servers:
  - name: github
    transport: stdio
    command: npx
    env:
      GITHUB_TOKEN: "${SHADOW_MCP_TEST_TOKEN}"
profiles: []
rules: []
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Simulate the CRUD path: load raw, mutate an unrelated field, save.
	raw, err := LoadRaw(path)
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	raw.DownstreamServers[0].CallTimeout = 0
	if err := Save(path, raw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(onDisk), "super-secret-value") {
		t.Fatalf("Save baked the resolved secret into the file on disk:\n%s", onDisk)
	}
	if !strings.Contains(string(onDisk), "${SHADOW_MCP_TEST_TOKEN}") {
		t.Fatalf("Save lost the ${VAR} placeholder entirely:\n%s", onDisk)
	}
}
