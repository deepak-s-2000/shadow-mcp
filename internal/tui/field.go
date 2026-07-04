package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldBool
	fieldEnum
)

// formField is one editable field in an entryForm. Only one of input/boolVal
// /options+optIdx is meaningful, depending on kind. visible lets a field hide
// itself based on other fields' current values (e.g. "command" only applies
// to a stdio server) - checked fresh on every render/navigation, not baked in
// at construction time, since the user can change the field it depends on.
type formField struct {
	key     string
	label   string
	kind    fieldKind
	input   textinput.Model
	boolVal bool
	options []string
	optIdx  int
	visible func(get func(key string) string) bool
}

func newTextField(key, label, value string) *formField {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetValue(value)
	return &formField{key: key, label: label, kind: fieldText, input: ti}
}

func newBoolField(key, label string, value bool) *formField {
	return &formField{key: key, label: label, kind: fieldBool, boolVal: value}
}

func newEnumField(key, label string, options []string, value string) *formField {
	idx := 0
	for i, o := range options {
		if o == value {
			idx = i
		}
	}
	return &formField{key: key, label: label, kind: fieldEnum, options: options, optIdx: idx}
}

// value returns the field's current value as a plain string, regardless of
// kind - used both for display and for other fields' visible() checks.
func (f *formField) value() string {
	switch f.kind {
	case fieldBool:
		if f.boolVal {
			return "true"
		}
		return "false"
	case fieldEnum:
		return f.options[f.optIdx]
	default:
		return f.input.Value()
	}
}

// handleKey applies one key event while this field is focused. handled is
// false for keys the field doesn't consume itself (tab/enter/esc), which the
// owning form interprets as navigation/submission instead.
func (f *formField) handleKey(msg tea.KeyMsg) (handled bool, cmd tea.Cmd) {
	switch f.kind {
	case fieldBool:
		switch msg.String() {
		case " ", "enter", "left", "right":
			f.boolVal = !f.boolVal
			return true, nil
		}
		return false, nil
	case fieldEnum:
		switch msg.String() {
		case "left", "h":
			f.optIdx = (f.optIdx - 1 + len(f.options)) % len(f.options)
			return true, nil
		case "right", "l", " ":
			f.optIdx = (f.optIdx + 1) % len(f.options)
			return true, nil
		}
		return false, nil
	default:
		switch msg.String() {
		case "tab", "shift+tab", "enter", "esc", "up", "down":
			return false, nil
		}
		var updated textinput.Model
		updated, cmd = f.input.Update(msg)
		f.input = updated
		return true, cmd
	}
}

func (f *formField) setFocus(focused bool) tea.Cmd {
	if f.kind != fieldText {
		return nil
	}
	if focused {
		return f.input.Focus()
	}
	f.input.Blur()
	return nil
}

func (f *formField) render(focused bool) string {
	label := fmt.Sprintf("%-14s", f.label+":")
	if focused {
		label = activeTabStyle.Render(label)
	} else {
		label = headerStyle.Render(label)
	}

	var value string
	switch f.kind {
	case fieldBool:
		box := "[ ]"
		if f.boolVal {
			box = "[x]"
		}
		value = box
	case fieldEnum:
		value = strings.Join(renderOptions(f.options, f.optIdx), "  ")
	default:
		value = f.input.View()
	}
	return label + " " + value
}

func renderOptions(options []string, selected int) []string {
	out := make([]string, len(options))
	for i, o := range options {
		if i == selected {
			out[i] = activeTabStyle.Render("(" + o + ")")
		} else {
			out[i] = dimStyle.Render(o)
		}
	}
	return out
}

var formBoxStyle = lipgloss.NewStyle().Padding(0, 1)
