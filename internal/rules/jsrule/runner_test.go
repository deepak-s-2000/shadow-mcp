package jsrule

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/rules"
)

func writeScript(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rule.js")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunner_MutatesArguments(t *testing.T) {
	script := writeScript(t, `
		function onCall(input) {
			var args = input.arguments;
			args.message = args.message + " (mutated)";
			return { action: "continue", arguments: args };
		}
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
	script := writeScript(t, `
		function onCall(input) {
			return { action: "reject", reason: "nope" };
		}
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

func TestRunner_TimesOutOnInfiniteLoop(t *testing.T) {
	script := writeScript(t, `
		function onCall(input) {
			while (true) {}
		}
	`)

	r := NewRunner()
	rule := &config.Rule{Name: "spin", Script: script, Timeout: 200 * time.Millisecond}

	start := time.Now()
	_, err := r.Run(context.Background(), rule, rules.Input{Hook: rules.HookPre})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run took %s, expected it to be bounded by the 200ms timeout", elapsed)
	}
}

func TestRunner_MalformedOutputErrors(t *testing.T) {
	script := writeScript(t, `
		function onCall(input) {
			return { action: "do-something-unknown" };
		}
	`)

	r := NewRunner()
	rule := &config.Rule{Name: "malformed", Script: script}
	_, err := r.Run(context.Background(), rule, rules.Input{Hook: rules.HookPre})
	if err == nil {
		t.Fatal("expected an error for an unknown action, got nil")
	}
}

func TestRunner_MissingOnCallErrors(t *testing.T) {
	script := writeScript(t, `var notAFunction = 42;`)

	r := NewRunner()
	rule := &config.Rule{Name: "missing", Script: script}
	_, err := r.Run(context.Background(), rule, rules.Input{Hook: rules.HookPre})
	if err == nil {
		t.Fatal("expected an error when onCall is not defined, got nil")
	}
}
