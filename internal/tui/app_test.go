package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/daemon"
)

func testModel() model {
	m := model{}
	m2, _ := m.Update(dataMsg{
		fetched: true,
		status: daemon.StatusResponse{
			Uptime:  "1m0s",
			Servers: []daemon.ServerHealth{{Name: "github", Status: "up"}, {Name: "linear", Status: "down: timeout"}},
		},
		calls: []daemon.CallRecord{
			{Profile: "vscode", ExposedTool: "github__echo", OK: true, RulesFired: []string{"mask-secret"}},
			{Profile: "cursor", ExposedTool: "github__delete", OK: false, Error: "rejected by rule: nope"},
		},
		cfg: daemon.ConfigSnapshot{
			DownstreamServers: []config.DownstreamServer{{Name: "github", Transport: "stdio", Command: "npx"}},
			Profiles: []config.Profile{{
				Name:     "vscode",
				Identify: config.ProfileIdentify{StdioArg: "vscode"},
				Tools:    config.ToolFilter{Allow: []string{"github__*"}},
			}},
			Rules: []config.Rule{{
				Name: "mask-secret", Script: "mask.py", Language: "python", Hooks: []string{"post"},
			}},
		},
	})
	return m2.(model)
}

func TestStatusTabRendersServersAndCalls(t *testing.T) {
	m := testModel()
	out := m.View()

	for _, want := range []string{"github", "up", "linear", "down: timeout", "vscode", "github__echo", "cursor", "github__delete"} {
		if !strings.Contains(out, want) {
			t.Errorf("status view missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestTabSwitching(t *testing.T) {
	m := testModel()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = next.(model)
	if m.tab != tabServers {
		t.Fatalf("tab = %v, want tabServers", m.tab)
	}
	if !strings.Contains(m.View(), "npx") {
		t.Errorf("servers view missing configured command, got:\n%s", m.View())
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = next.(model)
	if !strings.Contains(m.View(), "github__*") {
		t.Errorf("profiles view missing allowlist, got:\n%s", m.View())
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	m = next.(model)
	if !strings.Contains(m.View(), "mask-secret") {
		t.Errorf("rules view missing rule name, got:\n%s", m.View())
	}
}

func TestQuitOnQ(t *testing.T) {
	m := testModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected a quit command from 'q', got nil")
	}
}
