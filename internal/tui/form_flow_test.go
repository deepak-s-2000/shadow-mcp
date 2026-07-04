package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/daemon"
)

func testModelWithTwoServers() model {
	m := model{}
	m2, _ := m.Update(dataMsg{
		fetched: true,
		cfg: daemon.ConfigSnapshot{
			DownstreamServers: []config.DownstreamServer{
				{Name: "github", Transport: "stdio", Command: "npx"},
				{Name: "kite", Transport: "sse", URL: "https://mcp.kite.trade/sse"},
			},
		},
	})
	return m2.(model)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestListNavigationClampsAtBounds(t *testing.T) {
	m := testModelWithTwoServers()
	next, _ := m.Update(key("2")) // servers tab
	m = next.(model)

	if m.cursor[tabServers] != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor[tabServers])
	}

	next, _ = m.Update(key("down"))
	m = next.(model)
	if m.cursor[tabServers] != 1 {
		t.Fatalf("after down, cursor = %d, want 1", m.cursor[tabServers])
	}

	// One more "down" past the end should clamp, not wrap or go out of range.
	next, _ = m.Update(key("down"))
	m = next.(model)
	if m.cursor[tabServers] != 1 {
		t.Fatalf("cursor should clamp at len-1=1, got %d", m.cursor[tabServers])
	}

	next, _ = m.Update(key("up"))
	m = next.(model)
	if m.cursor[tabServers] != 0 {
		t.Fatalf("after up, cursor = %d, want 0", m.cursor[tabServers])
	}
}

func TestOpenNewFormThenCancel(t *testing.T) {
	m := testModelWithTwoServers()
	next, _ := m.Update(key("2"))
	m = next.(model)

	next, _ = m.Update(key("a"))
	m = next.(model)
	if m.mode != modeForm || m.form == nil {
		t.Fatalf("expected modeForm with a form after 'a', got mode=%v form=%v", m.mode, m.form)
	}
	if m.form.kind != entryServer || m.form.originalName != "" {
		t.Fatalf("expected a blank new-server form, got kind=%v originalName=%q", m.form.kind, m.form.originalName)
	}

	next, _ = m.Update(key("esc"))
	m = next.(model)
	if m.mode != modeList || m.form != nil {
		t.Fatalf("expected esc to cancel back to modeList with no form, got mode=%v form=%v", m.mode, m.form)
	}
}

func TestEditFetchedMsgOpensPrefilledForm(t *testing.T) {
	m := testModelWithTwoServers()
	next, _ := m.Update(key("2"))
	m = next.(model)

	next, _ = m.Update(editFetchedMsg{
		kind:   entryServer,
		server: config.DownstreamServer{Name: "kite", Transport: "sse", URL: "https://mcp.kite.trade/sse"},
	})
	m = next.(model)

	if m.mode != modeForm || m.form == nil {
		t.Fatalf("expected modeForm after editFetchedMsg, got mode=%v", m.mode)
	}
	if m.form.originalName != "kite" {
		t.Fatalf("expected an edit form for 'kite', originalName=%q", m.form.originalName)
	}
	if got := m.form.get("url"); got != "https://mcp.kite.trade/sse" {
		t.Fatalf("form not pre-filled with raw URL: got %q", got)
	}
	if got := m.form.get("transport"); got != "sse" {
		t.Fatalf("form transport = %q, want sse", got)
	}
}

func TestFormValidationErrorKeepsFormOpen(t *testing.T) {
	m := testModelWithTwoServers()
	next, _ := m.Update(key("2"))
	m = next.(model)
	next, _ = m.Update(key("a"))
	m = next.(model)

	// Leave "name" blank and try to submit via ctrl+s.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)

	if m.mode != modeForm || m.form == nil {
		t.Fatalf("expected the form to stay open after a validation error, got mode=%v", m.mode)
	}
	if m.form.err == "" {
		t.Fatalf("expected a validation error message, got none")
	}
	if m.form.submitting {
		t.Fatalf("submitting should stay false when collect() fails locally (no network round trip attempted)")
	}
}

func TestConfirmDeleteFlowCancel(t *testing.T) {
	m := testModelWithTwoServers()
	next, _ := m.Update(key("2"))
	m = next.(model)

	next, _ = m.Update(key("d"))
	m = next.(model)
	if m.mode != modeConfirmDelete {
		t.Fatalf("expected modeConfirmDelete after 'd', got %v", m.mode)
	}
	if m.pendingDeleteName != "github" {
		t.Fatalf("expected pending delete for the selected row 'github', got %q", m.pendingDeleteName)
	}
	if !strings.Contains(m.View(), "Delete server \"github\"?") {
		t.Errorf("confirm-delete prompt not shown, got:\n%s", m.View())
	}

	next, _ = m.Update(key("n"))
	m = next.(model)
	if m.mode != modeList {
		t.Fatalf("expected 'n' to cancel back to modeList, got %v", m.mode)
	}
}

func TestNewServerFormHidesHTTPFieldsForStdio(t *testing.T) {
	f := newServerForm(nil) // defaults to transport=stdio
	visible := f.visibleFields()
	for _, fl := range visible {
		if fl.key == "url" || fl.key == "headers" {
			t.Errorf("url/headers should be hidden for transport=stdio, but %q is visible", fl.key)
		}
	}

	// Switch transport to http (enum field, cycles with "right") and confirm
	// url/headers become visible while command/args/env hide instead.
	transportField := f.field("transport")
	transportField.handleKey(key("right"))
	if got := transportField.value(); got != "http" {
		t.Fatalf("transport = %q after cycling, want http", got)
	}

	visible = f.visibleFields()
	var sawURL, sawCommand bool
	for _, fl := range visible {
		if fl.key == "url" {
			sawURL = true
		}
		if fl.key == "command" {
			sawCommand = true
		}
	}
	if !sawURL {
		t.Errorf("url field should be visible once transport=http")
	}
	if sawCommand {
		t.Errorf("command field should be hidden once transport=http")
	}
}

func TestFormTypingIntoNameField(t *testing.T) {
	f := newRuleForm(nil)
	f.init()

	for _, r := range "my-rule" {
		f.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	if got := f.get("name"); got != "my-rule" {
		t.Fatalf("typed name = %q, want %q", got, "my-rule")
	}

	// Tab should move focus to the next visible field ("script") without
	// inserting a literal tab character into the name field.
	f.update(key("tab"))
	if got := f.get("name"); got != "my-rule" {
		t.Fatalf("name field mutated by tab: %q", got)
	}
	visible := f.visibleFields()
	if visible[f.focus].key != "script" {
		t.Fatalf("focus after tab = %q, want script", visible[f.focus].key)
	}
}

func TestConfirmDeleteFlowConfirmReturnsCommand(t *testing.T) {
	m := testModelWithTwoServers()
	next, _ := m.Update(key("2"))
	m = next.(model)
	next, _ = m.Update(key("d"))
	m = next.(model)

	next, cmd := m.Update(key("y"))
	m = next.(model)
	if m.mode != modeList {
		t.Fatalf("expected modeList immediately after confirming (delete runs async), got %v", m.mode)
	}
	if cmd == nil {
		t.Fatalf("expected a delete command to be returned")
	}
}
