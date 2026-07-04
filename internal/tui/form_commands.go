package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shadow-code/shadow-mcp/internal/adminclient"
	"github.com/shadow-code/shadow-mcp/internal/config"
)

// formResultMsg reports the outcome of a create/update/delete admin-API
// call. err == nil means it succeeded and the daemon has already reloaded.
type formResultMsg struct{ err error }

// editFetchedMsg carries the raw (pre-interpolation) entity fetched before
// opening an edit form - see GetServer/GetProfile/GetRule's doc comment on
// why the form must never be pre-filled from the redacted ConfigSnapshot.
type editFetchedMsg struct {
	kind    entryKind
	server  config.DownstreamServer
	profile config.Profile
	rule    config.Rule
	err     error
}

func fetchServerCmd(admin *adminclient.Client, name string) tea.Cmd {
	return func() tea.Msg {
		s, err := admin.GetServer(name)
		return editFetchedMsg{kind: entryServer, server: s, err: err}
	}
}

func fetchProfileCmd(admin *adminclient.Client, name string) tea.Cmd {
	return func() tea.Msg {
		p, err := admin.GetProfile(name)
		return editFetchedMsg{kind: entryProfile, profile: p, err: err}
	}
}

func fetchRuleCmd(admin *adminclient.Client, name string) tea.Cmd {
	return func() tea.Msg {
		r, err := admin.GetRule(name)
		return editFetchedMsg{kind: entryRule, rule: r, err: err}
	}
}

func submitServerCmd(admin *adminclient.Client, originalName string, s config.DownstreamServer) tea.Cmd {
	return func() tea.Msg {
		var err error
		if originalName == "" {
			err = admin.CreateServer(s)
		} else {
			err = admin.UpdateServer(originalName, s)
		}
		return formResultMsg{err: err}
	}
}

func submitProfileCmd(admin *adminclient.Client, originalName string, p config.Profile) tea.Cmd {
	return func() tea.Msg {
		var err error
		if originalName == "" {
			err = admin.CreateProfile(p)
		} else {
			err = admin.UpdateProfile(originalName, p)
		}
		return formResultMsg{err: err}
	}
}

func submitRuleCmd(admin *adminclient.Client, originalName string, r config.Rule) tea.Cmd {
	return func() tea.Msg {
		var err error
		if originalName == "" {
			err = admin.CreateRule(r)
		} else {
			err = admin.UpdateRule(originalName, r)
		}
		return formResultMsg{err: err}
	}
}

func deleteEntryCmd(admin *adminclient.Client, kind entryKind, name string) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch kind {
		case entryServer:
			err = admin.DeleteServer(name)
		case entryProfile:
			err = admin.DeleteProfile(name)
		case entryRule:
			err = admin.DeleteRule(name)
		}
		return formResultMsg{err: err}
	}
}
