package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type entryKind int

const (
	entryServer entryKind = iota
	entryProfile
	entryRule
)

func (k entryKind) String() string {
	return [...]string{"server", "profile", "rule"}[k]
}

// entryForm is the shared add/edit form for all three entity kinds -
// servers, profiles, rules each build one via newServerForm/newProfileForm/
// newRuleForm (form_convert.go) and collect it back into a config struct the
// same way, so the navigation/rendering/validation-error handling only
// needs to be written once.
type entryForm struct {
	kind         entryKind
	originalName string // "" means creating a new entry
	fields       []*formField
	focus        int
	err          string
	submitting   bool
}

// get looks up a field's current value by key, for another field's visible()
// check (e.g. "command" is only visible when transport == "stdio").
func (f *entryForm) get(key string) string {
	for _, fl := range f.fields {
		if fl.key == key {
			return fl.value()
		}
	}
	return ""
}

func (f *entryForm) field(key string) *formField {
	for _, fl := range f.fields {
		if fl.key == key {
			return fl
		}
	}
	return nil
}

// visibleFields returns the currently-visible fields in order, re-evaluated
// every call since visibility can depend on another field the user just changed.
func (f *entryForm) visibleFields() []*formField {
	var out []*formField
	for _, fl := range f.fields {
		if fl.visible == nil || fl.visible(f.get) {
			out = append(out, fl)
		}
	}
	return out
}

func (f *entryForm) init() tea.Cmd {
	visible := f.visibleFields()
	if len(visible) == 0 {
		return nil
	}
	return visible[0].setFocus(true)
}

// update handles one message while the form is active. submitted is true
// only on a successful "enter on the last action" (the caller performs the
// actual save and closes the form); cancelled is true on esc.
func (f *entryForm) update(msg tea.Msg) (cmd tea.Cmd, submitted, cancelled bool) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false, false
	}

	visible := f.visibleFields()
	if len(visible) == 0 {
		return nil, false, false
	}
	if f.focus >= len(visible) {
		f.focus = len(visible) - 1
	}
	current := visible[f.focus]

	switch keyMsg.String() {
	case "esc":
		return nil, false, true
	case "ctrl+s":
		return nil, true, false
	}

	if handled, cmd := current.handleKey(keyMsg); handled {
		return cmd, false, false
	}

	switch keyMsg.String() {
	case "tab", "down":
		return f.moveFocus(visible, 1), false, false
	case "shift+tab", "up":
		return f.moveFocus(visible, -1), false, false
	case "enter":
		if f.focus == len(visible)-1 {
			return nil, true, false
		}
		return f.moveFocus(visible, 1), false, false
	}
	return nil, false, false
}

func (f *entryForm) moveFocus(visible []*formField, delta int) tea.Cmd {
	visible[f.focus].setFocus(false)
	f.focus = (f.focus + delta + len(visible)) % len(visible)
	return visible[f.focus].setFocus(true)
}

func (f *entryForm) title() string {
	verb := "Edit"
	if f.originalName == "" {
		verb = "New"
	}
	return verb + " " + f.kind.String() + ":"
}

func (f *entryForm) view() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(f.title()))
	b.WriteString("\n\n")

	visible := f.visibleFields()
	for i, fl := range visible {
		b.WriteString(fl.render(i == f.focus))
		b.WriteString("\n")
	}

	if f.err != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render("error: " + f.err))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("tab/↓ next field   shift+tab/↑ prev   enter (on last field) or ctrl+s: save   esc: cancel"))
	return formBoxStyle.Render(b.String())
}
