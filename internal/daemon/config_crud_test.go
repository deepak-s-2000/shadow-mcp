package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shadow-code/shadow-mcp/internal/config"
)

func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "shadow-mcp.yaml")
	if err := os.WriteFile(path, []byte("downstream_servers: []\nprofiles: []\nrules: []\n"), 0o600); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	t.Setenv("APPDATA", dir) // isolate usage.json/lockfile location from the real machine state
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	d, err := New(context.Background(), path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestUpsertProfileCreateThenRename(t *testing.T) {
	d := newTestDaemon(t)
	ctx := context.Background()

	p := config.Profile{Name: "vscode", Identify: config.ProfileIdentify{StdioArg: "vscode"}, Tools: config.ToolFilter{Allow: []string{"*"}}}
	if err := d.UpsertProfile(ctx, "", p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, ok := d.RawProfile("vscode"); !ok || got.Identify.StdioArg != "vscode" {
		t.Fatalf("RawProfile after create = %+v, ok=%v", got, ok)
	}

	// Rename vscode -> vscode-2 via update.
	p.Name = "vscode-2"
	if err := d.UpsertProfile(ctx, "vscode", p); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, ok := d.RawProfile("vscode"); ok {
		t.Fatalf("old name %q should no longer exist after rename", "vscode")
	}
	if _, ok := d.RawProfile("vscode-2"); !ok {
		t.Fatalf("renamed profile %q not found", "vscode-2")
	}
}

func TestUpsertProfileRejectsDuplicateName(t *testing.T) {
	d := newTestDaemon(t)
	ctx := context.Background()

	a := config.Profile{Name: "a", Identify: config.ProfileIdentify{StdioArg: "a"}}
	b := config.Profile{Name: "b", Identify: config.ProfileIdentify{StdioArg: "b"}}
	if err := d.UpsertProfile(ctx, "", a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := d.UpsertProfile(ctx, "", b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	// Renaming b to collide with a's name must fail, and must not have
	// clobbered the on-disk config (b should still be named "b").
	collide := config.Profile{Name: "a", Identify: config.ProfileIdentify{StdioArg: "b"}}
	if err := d.UpsertProfile(ctx, "b", collide); err == nil {
		t.Fatalf("expected a collision error, got nil")
	}
	if _, ok := d.RawProfile("b"); !ok {
		t.Fatalf("profile 'b' should still exist after a failed rename attempt")
	}
}

func TestDeleteProfileNotFound(t *testing.T) {
	d := newTestDaemon(t)
	if err := d.DeleteProfile(context.Background(), "does-not-exist"); err == nil {
		t.Fatalf("expected an error deleting a nonexistent profile")
	}
}

func TestUpsertRuleCreateAndDelete(t *testing.T) {
	d := newTestDaemon(t)
	ctx := context.Background()

	scriptPath := filepath.Join(t.TempDir(), "rule.js")
	if err := os.WriteFile(scriptPath, []byte("function onCall(input) { return {action:'continue'}; }"), 0o600); err != nil {
		t.Fatalf("seeding rule script: %v", err)
	}

	r := config.Rule{Name: "mask", Script: scriptPath, Language: "js", Hooks: []string{"post"}}
	if err := d.UpsertRule(ctx, "", r); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := d.RawRule("mask"); !ok {
		t.Fatalf("rule not found after create")
	}

	if err := d.DeleteRule(ctx, "mask"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := d.RawRule("mask"); ok {
		t.Fatalf("rule still present after delete")
	}
}

func TestUpsertRuleRejectsInvalidLanguage(t *testing.T) {
	d := newTestDaemon(t)
	r := config.Rule{Name: "bad", Script: "x.js", Language: "ruby", Hooks: []string{"pre"}}
	if err := d.UpsertRule(context.Background(), "", r); err == nil {
		t.Fatalf("expected a validation error for an unsupported language")
	}
	if _, ok := d.RawRule("bad"); ok {
		t.Fatalf("invalid rule should not have been persisted")
	}
}

func TestUpsertServerRejectsMissingCommand(t *testing.T) {
	d := newTestDaemon(t)
	s := config.DownstreamServer{Name: "broken", Transport: "stdio"} // no Command
	if err := d.UpsertServer(context.Background(), "", s); err == nil {
		t.Fatalf("expected a validation error for a stdio server with no command")
	}
	if _, ok := d.RawServer("broken"); ok {
		t.Fatalf("invalid server should not have been persisted")
	}
}

// TestUpsertServerRollsBackOnReloadFailure covers a structurally-valid server
// (passes config.Validate) that nonetheless can't actually connect, so the
// reload inside mutateRaw fails after config.Save already wrote it to disk.
// The file must be rolled back - otherwise the broken entry stays on disk
// and every later reload (including from unrelated future edits) keeps
// failing on it.
func TestUpsertServerRollsBackOnReloadFailure(t *testing.T) {
	d := newTestDaemon(t)
	ctx := context.Background()

	before, err := os.ReadFile(d.configPath)
	if err != nil {
		t.Fatalf("reading config before edit: %v", err)
	}

	// A command that exits immediately without speaking MCP - passes
	// validation (stdio + non-empty command) but fails to connect.
	broken := config.DownstreamServer{Name: "broken", Transport: "stdio", Command: "cmd", Args: []string{"/c", "exit", "0"}}
	if err := d.UpsertServer(ctx, "", broken); err == nil {
		t.Fatalf("expected a connect failure for a non-MCP-speaking command")
	}

	after, err := os.ReadFile(d.configPath)
	if err != nil {
		t.Fatalf("reading config after failed edit: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("config file was not rolled back after a failed reload:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if _, ok := d.RawServer("broken"); ok {
		t.Fatalf("broken server should not be present in memory after rollback")
	}

	// The daemon must still be usable afterwards - a later, valid edit
	// should succeed rather than being permanently wedged by the earlier failure.
	good := config.Profile{Name: "vscode", Identify: config.ProfileIdentify{StdioArg: "vscode"}}
	if err := d.UpsertProfile(ctx, "", good); err != nil {
		t.Fatalf("a valid edit after a rolled-back failure should still succeed: %v", err)
	}
}
