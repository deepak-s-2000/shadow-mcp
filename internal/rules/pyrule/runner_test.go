package pyrule

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/rules"
)

func requirePython(t *testing.T) string {
	t.Helper()
	r := NewRunner()
	if _, err := exec.LookPath(r.pythonPath()); err != nil {
		t.Skipf("%s not found on PATH, skipping", r.pythonPath())
	}
	return r.pythonPath()
}

func writeScript(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rule.py")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunner_MutatesArguments(t *testing.T) {
	requirePython(t)
	script := writeScript(t, `
import json, sys
data = json.load(sys.stdin)
args = data["arguments"]
args["message"] = args["message"] + " (mutated)"
json.dump({"action": "continue", "arguments": args}, sys.stdout)
`)

	r := NewRunner()
	rule := &config.Rule{Name: "mutate", Script: script}
	out, err := r.Run(context.Background(), rule, rules.Input{
		Hook:      rules.HookPre,
		Arguments: map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Action != rules.ActionContinue {
		t.Fatalf("action = %q, want continue", out.Action)
	}
	args, ok := out.Arguments.(map[string]any)
	if !ok || args["message"] != "hello (mutated)" {
		t.Fatalf("arguments = %#v, want message %q", out.Arguments, "hello (mutated)")
	}
}

func TestRunner_Rejects(t *testing.T) {
	requirePython(t)
	script := writeScript(t, `
import json, sys
json.load(sys.stdin)
json.dump({"action": "reject", "reason": "nope"}, sys.stdout)
`)

	r := NewRunner()
	rule := &config.Rule{Name: "reject", Script: script}
	out, err := r.Run(context.Background(), rule, rules.Input{Hook: rules.HookPre})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Action != rules.ActionReject || out.Reason != "nope" {
		t.Fatalf("output = %#v, want reject with reason %q", out, "nope")
	}
}

func TestRunner_TimesOut(t *testing.T) {
	requirePython(t)
	script := writeScript(t, `
import time
time.sleep(30)
`)

	r := NewRunner()
	rule := &config.Rule{Name: "spin", Script: script, Timeout: 300 * time.Millisecond}

	start := time.Now()
	_, err := r.Run(context.Background(), rule, rules.Input{Hook: rules.HookPre})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %s, expected it to be bounded by the 300ms timeout", elapsed)
	}
}

func TestRunner_EnvIsolation(t *testing.T) {
	requirePython(t)
	t.Setenv("SHADOW_MCP_TEST_SECRET", "should-not-be-visible")

	script := writeScript(t, `
import json, os, sys
json.load(sys.stdin)
leaked = "SHADOW_MCP_TEST_SECRET" in os.environ
json.dump({"action": "continue", "arguments": {"leaked": leaked}}, sys.stdout)
`)

	r := NewRunner()
	rule := &config.Rule{Name: "envcheck", Script: script} // no Env allowlist entry for the secret
	out, err := r.Run(context.Background(), rule, rules.Input{Hook: rules.HookPre})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	args, ok := out.Arguments.(map[string]any)
	if !ok || args["leaked"] != false {
		t.Fatalf("expected the unallowlisted env var to be invisible to the script, got %#v", out.Arguments)
	}
}
