// Package tui implements `shadow-mcp ui`: a terminal dashboard for viewing
// live daemon status and editing the currently configured servers/profiles/rules.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shadow-code/shadow-mcp/internal/adminclient"
	"github.com/shadow-code/shadow-mcp/internal/daemon"
)

const pollInterval = 1500 * time.Millisecond

var (
	tabBarStyle    = lipgloss.NewStyle().MarginBottom(1)
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Underline(true)
	inactiveStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	headerStyle    = lipgloss.NewStyle().Bold(true)
	upStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	downStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	errStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
)

type tab int

const (
	tabStatus tab = iota
	tabServers
	tabProfiles
	tabRules
	tabCount
)

func (t tab) String() string {
	return [...]string{"Status", "Servers", "Profiles", "Rules"}[t]
}

// uiMode selects which of three mutually-exclusive interaction modes the TUI
// is in: browsing lists, filling out an add/edit form, or confirming a delete.
type uiMode int

const (
	modeList uiMode = iota
	modeForm
	modeConfirmDelete
)

type model struct {
	admin *adminclient.Client
	tab   tab
	mode  uiMode

	status  daemon.StatusResponse
	calls   []daemon.CallRecord
	cfg     daemon.ConfigSnapshot
	lastErr error

	cursor [tabCount]int

	form *entryForm

	pendingDeleteKind entryKind
	pendingDeleteName string

	width, height int
}

// Run connects to (auto-starting if needed) the daemon serving configPath and
// runs the interactive dashboard until the user quits.
func Run(configPath string) error {
	admin, err := adminclient.EnsureRunning(configPath)
	if err != nil {
		return fmt.Errorf("connecting to shadow-mcp daemon: %w", err)
	}

	m := model{admin: admin}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchAll(m.admin), tick())
}

type tickMsg time.Time
type dataMsg struct {
	status  daemon.StatusResponse
	calls   []daemon.CallRecord
	cfg     daemon.ConfigSnapshot
	fetched bool
	err     error
}

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func fetchAll(admin *adminclient.Client) tea.Cmd {
	return func() tea.Msg {
		status, err := admin.Status()
		if err != nil {
			return dataMsg{err: err}
		}
		calls, err := admin.RecentCalls(50)
		if err != nil {
			return dataMsg{err: err}
		}
		cfg, err := admin.Config()
		if err != nil {
			return dataMsg{err: err}
		}
		return dataMsg{status: status, calls: calls, cfg: cfg, fetched: true}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeForm:
			return m.updateForm(msg)
		case modeConfirmDelete:
			return m.updateConfirmDelete(msg)
		default:
			return m.updateList(msg)
		}

	case tickMsg:
		return m, tea.Batch(fetchAll(m.admin), tick())

	case dataMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			return m, nil
		}
		m.lastErr = nil
		m.status, m.calls, m.cfg = msg.status, msg.calls, msg.cfg
		return m, nil

	case editFetchedMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			return m, nil
		}
		switch msg.kind {
		case entryServer:
			m.form = newServerForm(&msg.server)
		case entryProfile:
			m.form = newProfileForm(&msg.profile)
		case entryRule:
			m.form = newRuleForm(&msg.rule)
		}
		m.mode = modeForm
		return m, m.form.init()

	case formResultMsg:
		if msg.err != nil {
			if m.form != nil {
				m.form.submitting = false
				m.form.err = msg.err.Error()
			} else {
				m.lastErr = msg.err
			}
			return m, nil
		}
		m.mode = modeList
		m.form = nil
		return m, fetchAll(m.admin)
	}

	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "left", "h", "shift+tab":
		m.tab = (m.tab - 1 + tabCount) % tabCount
	case "right", "l", "tab":
		m.tab = (m.tab + 1) % tabCount
	case "1":
		m.tab = tabStatus
	case "2":
		m.tab = tabServers
	case "3":
		m.tab = tabProfiles
	case "4":
		m.tab = tabRules
	case "r":
		return m, fetchAll(m.admin)
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "a":
		return m.openNewForm()
	case "e", "enter":
		return m.openEditForm()
	case "d":
		return m.openConfirmDelete()
	}
	return m, nil
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.form == nil || m.form.submitting {
		return m, nil
	}

	cmd, submitted, cancelled := m.form.update(msg)
	if cancelled {
		m.mode = modeList
		m.form = nil
		return m, nil
	}
	if submitted {
		switch m.form.kind {
		case entryServer:
			s, err := m.form.collectServer()
			if err != nil {
				m.form.err = err.Error()
				return m, nil
			}
			m.form.submitting = true
			return m, submitServerCmd(m.admin, m.form.originalName, s)
		case entryProfile:
			p, err := m.form.collectProfile()
			if err != nil {
				m.form.err = err.Error()
				return m, nil
			}
			m.form.submitting = true
			return m, submitProfileCmd(m.admin, m.form.originalName, p)
		case entryRule:
			r, err := m.form.collectRule()
			if err != nil {
				m.form.err = err.Error()
				return m, nil
			}
			m.form.submitting = true
			return m, submitRuleCmd(m.admin, m.form.originalName, r)
		}
	}
	return m, cmd
}

func (m model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.mode = modeList
		return m, deleteEntryCmd(m.admin, m.pendingDeleteKind, m.pendingDeleteName)
	case "n", "esc":
		m.mode = modeList
	}
	return m, nil
}

// listLen returns how many rows the current tab's list has, or 0 for tabs
// with no list (Status).
func (m model) listLen() int {
	switch m.tab {
	case tabServers:
		return len(m.cfg.DownstreamServers)
	case tabProfiles:
		return len(m.cfg.Profiles)
	case tabRules:
		return len(m.cfg.Rules)
	default:
		return 0
	}
}

func (m *model) moveCursor(delta int) {
	n := m.listLen()
	if n == 0 {
		m.cursor[m.tab] = 0
		return
	}
	c := m.cursor[m.tab] + delta
	if c < 0 {
		c = 0
	}
	if c >= n {
		c = n - 1
	}
	m.cursor[m.tab] = c
}

func (m model) openNewForm() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabServers:
		m.form = newServerForm(nil)
	case tabProfiles:
		m.form = newProfileForm(nil)
	case tabRules:
		m.form = newRuleForm(nil)
	default:
		return m, nil
	}
	m.mode = modeForm
	return m, m.form.init()
}

// openEditForm fetches the RAW (pre-interpolation) entity from the daemon
// before opening the form - never pre-fills from m.cfg, which holds the
// redacted ConfigSnapshot used for read-only display. Pre-filling from that
// would show "***" as an env/header value, and resubmitting unchanged would
// permanently overwrite the real value (e.g. a "${GITHUB_TOKEN}" placeholder)
// with the literal string "***" on disk.
func (m model) openEditForm() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabServers:
		if len(m.cfg.DownstreamServers) == 0 {
			return m, nil
		}
		name := m.cfg.DownstreamServers[m.cursor[tabServers]].Name
		return m, fetchServerCmd(m.admin, name)
	case tabProfiles:
		if len(m.cfg.Profiles) == 0 {
			return m, nil
		}
		name := m.cfg.Profiles[m.cursor[tabProfiles]].Name
		return m, fetchProfileCmd(m.admin, name)
	case tabRules:
		if len(m.cfg.Rules) == 0 {
			return m, nil
		}
		name := m.cfg.Rules[m.cursor[tabRules]].Name
		return m, fetchRuleCmd(m.admin, name)
	}
	return m, nil
}

func (m model) openConfirmDelete() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabServers:
		if len(m.cfg.DownstreamServers) == 0 {
			return m, nil
		}
		m.pendingDeleteKind = entryServer
		m.pendingDeleteName = m.cfg.DownstreamServers[m.cursor[tabServers]].Name
	case tabProfiles:
		if len(m.cfg.Profiles) == 0 {
			return m, nil
		}
		m.pendingDeleteKind = entryProfile
		m.pendingDeleteName = m.cfg.Profiles[m.cursor[tabProfiles]].Name
	case tabRules:
		if len(m.cfg.Rules) == 0 {
			return m, nil
		}
		m.pendingDeleteKind = entryRule
		m.pendingDeleteName = m.cfg.Rules[m.cursor[tabRules]].Name
	default:
		return m, nil
	}
	m.mode = modeConfirmDelete
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(tabBarStyle.Render(m.renderTabBar()))
	b.WriteString("\n")

	if m.mode == modeForm && m.form != nil {
		b.WriteString(m.form.view())
		return b.String()
	}

	switch m.tab {
	case tabStatus:
		b.WriteString(m.renderStatus())
	case tabServers:
		b.WriteString(m.renderServers())
	case tabProfiles:
		b.WriteString(m.renderProfiles())
	case tabRules:
		b.WriteString(m.renderRules())
	}

	if m.mode == modeConfirmDelete {
		b.WriteString("\n\n")
		b.WriteString(errStyle.Render(fmt.Sprintf("Delete %s %q? (y/n)", m.pendingDeleteKind, m.pendingDeleteName)))
	}

	if m.lastErr != nil {
		b.WriteString("\n\n")
		b.WriteString(errStyle.Render("error: " + m.lastErr.Error()))
	}

	b.WriteString("\n\n")
	switch m.tab {
	case tabServers, tabProfiles, tabRules:
		b.WriteString(dimStyle.Render("↑/↓ select   a add   e/enter edit   d delete   tab switch tabs   r refresh   q quit"))
	default:
		b.WriteString(dimStyle.Render("tab/←→: switch tabs   r: refresh now   q: quit"))
	}
	return b.String()
}

func (m model) renderTabBar() string {
	var parts []string
	for t := tab(0); t < tabCount; t++ {
		label := fmt.Sprintf("%d:%s", t+1, t)
		if t == m.tab {
			parts = append(parts, activeTabStyle.Render(label))
		} else {
			parts = append(parts, inactiveStyle.Render(label))
		}
	}
	return strings.Join(parts, "   ")
}

func (m model) renderStatus() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Downstream servers") + fmt.Sprintf("  (uptime %s)\n", m.status.Uptime))
	for _, s := range m.status.Servers {
		style := upStyle
		if !strings.HasPrefix(s.Status, "up") {
			style = downStyle
		}
		b.WriteString(fmt.Sprintf("  %-20s %s\n", s.Name, style.Render(s.Status)))
	}
	if len(m.status.Servers) == 0 {
		b.WriteString(dimStyle.Render("  (none)\n"))
	}

	b.WriteString("\n" + headerStyle.Render("Recent calls") + "\n")
	if len(m.calls) == 0 {
		b.WriteString(dimStyle.Render("  (none yet)\n"))
	}
	for i, c := range m.calls {
		if i >= 15 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more\n", len(m.calls)-15)))
			break
		}
		okStyle := upStyle
		mark := "ok"
		if !c.OK {
			okStyle = downStyle
			mark = "ERR"
		}
		line := fmt.Sprintf("  %s  %-10s %-30s %s", c.Timestamp.Format("15:04:05"), c.Profile, c.ExposedTool, okStyle.Render(mark))
		if len(c.RulesFired) > 0 {
			line += dimStyle.Render("  rules: " + strings.Join(c.RulesFired, ", "))
		}
		if c.Error != "" {
			line += errStyle.Render("  " + c.Error)
		}
		b.WriteString(line + "\n")
	}

	return b.String()
}

func rowPrefix(selected bool) string {
	if selected {
		return selectedStyle.Render("> ")
	}
	return "  "
}

func (m model) renderServers() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Downstream servers") + "\n")
	for i, s := range m.cfg.DownstreamServers {
		selected := i == m.cursor[tabServers]
		b.WriteString(rowPrefix(selected) + fmt.Sprintf("%-15s transport=%-6s namespace=%v\n", s.Name, s.Transport, s.NamespaceEnabled()))
		if s.Transport == "stdio" {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    command: %s %s\n", s.Command, strings.Join(s.Args, " "))))
		} else {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    url: %s\n", s.URL)))
		}
	}
	if len(m.cfg.DownstreamServers) == 0 {
		b.WriteString(dimStyle.Render("  (none configured - press 'a' to add one)\n"))
	}
	return b.String()
}

func (m model) renderProfiles() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Profiles") + "\n")
	for i, p := range m.cfg.Profiles {
		selected := i == m.cursor[tabProfiles]
		b.WriteString(rowPrefix(selected) + p.Name + "\n")
		if p.Identify.StdioArg != "" {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    stdio_arg: %s\n", p.Identify.StdioArg)))
		}
		if p.Identify.HTTPPath != "" {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    http_path: %s\n", p.Identify.HTTPPath)))
		}
		b.WriteString(dimStyle.Render(fmt.Sprintf("    allow: %v\n", p.Tools.Allow)))
		if len(p.Tools.Deny) > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    deny:  %v\n", p.Tools.Deny)))
		}
		if p.LazyLoad != nil && p.LazyLoad.Enabled {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    lazy_load: enabled, limit=%d (ranked by usage frequency)\n", p.LazyLoad.EffectiveLimit())))
		}
	}
	if len(m.cfg.Profiles) == 0 {
		b.WriteString(dimStyle.Render("  (none configured - press 'a' to add one)\n"))
	}
	return b.String()
}

func (m model) renderRules() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Rules") + "\n")
	for i, r := range m.cfg.Rules {
		selected := i == m.cursor[tabRules]
		b.WriteString(rowPrefix(selected) + fmt.Sprintf("%-20s lang=%-6s hooks=%-10v priority=%d on_error=%s\n",
			r.Name, r.Language, r.Hooks, r.EffectivePriority(), r.OnErrorMode()))
		b.WriteString(dimStyle.Render(fmt.Sprintf("    script: %s\n", r.Script)))
		b.WriteString(dimStyle.Render(fmt.Sprintf("    applies_to: tools=%v servers=%v\n", r.AppliesTo.Tools, r.AppliesTo.Servers)))
	}
	if len(m.cfg.Rules) == 0 {
		b.WriteString(dimStyle.Render("  (none configured - press 'a' to add one)\n"))
	}
	return b.String()
}
