// Package tui is evr-ptero's terminal UI. It calls the same internal/core
// functions as the CLI.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/auth"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/supervisor"
	ui "github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/tui"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/updates"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// Wizard is the first-run setup; set by main to the panel installer flow.
var Wizard func(m *Model) tea.Cmd

// Run starts the TUI.
func Run(ctx context.Context, dir string) error {
	m := New(ctx, dir)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
		return nil
	}
	if err == nil {
		return reexecIfUpdated(m)
	}
	return err
}

type screen int

const (
	scrDashboard screen = iota
	scrLogs
	scrForm
	scrBusy
	scrAccount
	scrRuntime
	scrInfo
	scrUpdates
)

// Model is the TUI state.
type Model struct {
	Ctx           context.Context
	Dir           string
	App           *core.App
	width, height int
	screen        screen

	servers []core.Server
	table   table.Model
	loadErr error

	logs      ui.LogView
	logName   string
	logCancel context.CancelFunc

	form     *huh.Form
	onSubmit func() tea.Cmd
	back     screen

	busyTitle string
	busyLines []string
	busyDone  bool
	busyErr   error
	afterBusy func() tea.Cmd
	// busyRetry is offered with l when the task failed (the device link).
	busyRetry func() tea.Cmd
	// busyCancel stops the task on esc when busyCanCancel is set.
	busyCancel    context.CancelFunc
	busyCanCancel bool
	spin          spinner.Model

	account *accountInfo

	rtTable  table.Model
	runtimes []nevr.Installed

	infoTitle, infoText string

	updTable    table.Model
	updComps    []updates.Component
	updChecking bool
	reexec      bool

	// legacy are old-egg servers the dashboard points at; scanned once.
	legacy        []core.LegacyServer
	legacyScanned bool

	confirm       string
	confirmFunc   func() tea.Cmd
	flash         string
	flashBad      bool
	pendingWizard bool
}

type accountInfo struct {
	session auth.Session
	servers []auth.Server
	err     error
}

// New builds the model; without a config it starts the setup wizard.
func New(ctx context.Context, dir string) *Model {
	m := &Model{Ctx: ctx, Dir: dir, spin: spinner.New(spinner.WithSpinner(spinner.Dot))}
	m.table = table.New(table.WithColumns(columns(110)), table.WithFocused(true), table.WithHeight(10))
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).Foreground(ui.Accent).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true)
	s.Selected = s.Selected.Foreground(lipgloss.Color("#FFFFFF")).Background(ui.Accent).Bold(false)
	m.table.SetStyles(s)
	m.rtTable = table.New(table.WithColumns([]table.Column{{Title: "VERSION", Width: 14}, {Title: "STATE", Width: 12}, {Title: "INSTALLED", Width: 12}, {Title: "NOTE", Width: 40}}),
		table.WithFocused(true), table.WithHeight(8))
	m.rtTable.SetStyles(s)
	m.updTable = table.New(table.WithColumns([]table.Column{{Title: "PART", Width: 18}, {Title: "INSTALLED", Width: 16}, {Title: "AVAILABLE", Width: 34}, {Title: "MODE", Width: 7}, {Title: "NOTE", Width: 30}}),
		table.WithFocused(true), table.WithHeight(7))
	m.updTable.SetStyles(s)
	m.logs = ui.NewLogView(func(l string) bool { return !supervisor.DefaultFilter(supervisor.Clean(l)) })
	app, err := core.Open(dir)
	switch {
	case err == nil:
		m.App = app
	case errors.Is(err, pcfg.ErrNotInitialized) && Wizard != nil:
		m.pendingWizard = true
	default:
		m.loadErr = err
	}
	return m
}

func columns(width int) []table.Column {
	return []table.Column{
		{Title: "NAME", Width: 16}, {Title: "ID", Width: 9}, {Title: "PORT", Width: 6}, {Title: "STATE", Width: 12},
		{Title: "RUNTIME", Width: 8}, {Title: "REGISTERED AS", Width: 21}, {Title: "MATCHES", Width: 7}, {Title: "NOTE", Width: max(width-95, 10)},
	}
}

type (
	tickMsg    struct{}
	serversMsg struct {
		list []core.Server
		err  error
	}
	busyLineMsg struct{ line string }
	busyDoneMsg struct{ err error }
	accountMsg  *accountInfo
	flashMsg    struct {
		text string
		bad  bool
	}
	chanMsg struct {
		msg tea.Msg
		ch  <-chan tea.Msg
	}
)

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, tea.RequestWindowSize}
	if m.pendingWizard {
		m.pendingWizard = false
		cmds = append(cmds, Wizard(m))
	}
	if m.App != nil {
		cmds = append(cmds, m.LoadServers())
	}
	return tea.Batch(cmds...)
}

func tick() tea.Cmd { return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }

// LoadServers refreshes the server list.
func (m *Model) LoadServers() tea.Cmd {
	app := m.App
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.Ctx, 20*time.Second)
		defer cancel()
		list, err := app.Servers(ctx)
		return serversMsg{list, err}
	}
}

func waitChan(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return chanMsg{msg, ch}
	}
}

// Run executes a long task on the busy screen.
func (m *Model) Run(title string, task func(ctx context.Context, say func(string)) error, after func() tea.Cmd) tea.Cmd {
	m.screen, m.busyTitle, m.busyLines, m.busyDone, m.busyErr, m.afterBusy = scrBusy, title, nil, false, nil, after
	m.busyRetry, m.busyCanCancel = nil, false
	ctx, cancel := context.WithCancel(m.Ctx)
	m.busyCancel = cancel
	ch := make(chan tea.Msg, 128)
	go func() {
		defer cancel()
		err := task(ctx, func(s string) { ch <- busyLineMsg{s} })
		ch <- busyDoneMsg{err}
		close(ch)
	}()
	return waitChan(ch)
}

func (m *Model) quick(done string, f func(ctx context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.Ctx, 2*time.Minute)
		defer cancel()
		if err := f(ctx); err != nil {
			return flashMsg{err.Error(), true}
		}
		return flashMsg{done, false}
	}
}

// ShowForm opens a form; onSubmit runs when it completes.
func (m *Model) ShowForm(f *huh.Form, onSubmit func() tea.Cmd) tea.Cmd {
	m.form = f.WithTheme(huh.ThemeFunc(huh.ThemeCharm)).WithWidth(min(max(m.width-4, 40), 90)).WithShowHelp(true)
	m.back = m.screen
	if m.back == scrForm || m.back == scrBusy {
		m.back = scrDashboard
	}
	m.screen, m.onSubmit = scrForm, onSubmit
	return m.form.Init()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.table.SetColumns(columns(msg.Width))
		m.table.SetWidth(msg.Width)
		m.table.SetHeight(max(msg.Height-12, 4))
		m.rtTable.SetWidth(msg.Width)
		m.updTable.SetWidth(msg.Width)
		m.updTable.SetHeight(max(min(msg.Height-12, 9), 4))
		m.rtTable.SetHeight(max(msg.Height-14, 4))
		m.logs.SetSize(msg.Width, msg.Height-4)
		if m.form != nil {
			m.form = m.form.WithWidth(min(msg.Width-4, 90))
		}
		return m, nil
	case chanMsg:
		next, cmd := m.Update(msg.msg)
		return next, tea.Batch(cmd, waitChan(msg.ch))
	case tickMsg:
		if m.App != nil && m.screen == scrDashboard {
			return m, m.LoadServers()
		}
		return m, tick()
	case serversMsg:
		m.servers, m.loadErr = msg.list, msg.err
		m.refresh()
		if !m.legacyScanned && msg.err == nil && m.App != nil {
			m.legacyScanned = true
			return m, tea.Batch(tick(), m.scanLegacy())
		}
		return m, tick()
	case busyLineMsg:
		m.busyLines = append(m.busyLines, msg.line)
		return m, nil
	case busyDoneMsg:
		m.busyDone, m.busyErr = true, msg.err
		var lf linkFailed
		if errors.As(msg.err, &lf) && m.App != nil {
			m.busyRetry = m.linkDevice
		}
		return m, nil
	case runtimesMsg:
		m.runtimes = msg.list
		if msg.err != nil {
			m.flash, m.flashBad = msg.err.Error(), true
		}
		m.refreshRuntimes()
		return m, nil
	case legacyMsg:
		return m, m.migrateForm(msg)
	case legacyScanMsg:
		m.legacy = msg
		return m, nil
	case versionsMsg:
		return m, m.applyForm(msg)
	case updatesMsg:
		m.updChecking = false
		if m.screen == scrUpdates {
			m.refreshUpdates()
		}
		return m, nil
	case accountMsg:
		m.account = msg
		return m, nil
	case flashMsg:
		m.flash, m.flashBad = msg.text, msg.bad
		return m, m.LoadServers()
	case ui.LogLineMsg:
		if msg.Source == m.logName {
			m.logs.Append(msg.Line)
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.stopLogs()
			return m, tea.Quit
		}
		if m.confirm != "" {
			f := m.confirmFunc
			m.confirm, m.confirmFunc = "", nil
			if msg.String() == "y" {
				return m, f()
			}
			m.flash, m.flashBad = "cancelled", false
			return m, nil
		}
	}
	switch m.screen {
	case scrForm:
		if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" && m.App != nil {
			m.screen, m.form = m.back, nil
			return m, nil
		}
		next, cmd := m.form.Update(msg)
		if f, ok := next.(*huh.Form); ok {
			m.form = f
		}
		switch m.form.State {
		case huh.StateCompleted:
			submit := m.onSubmit
			m.form, m.screen = nil, m.back
			return m, tea.Batch(cmd, submit())
		case huh.StateAborted:
			if m.App == nil {
				return m, tea.Quit
			}
			m.form, m.screen = nil, m.back
		}
		return m, cmd
	case scrBusy:
		k, ok := msg.(tea.KeyPressMsg)
		if ok && !m.busyDone && k.String() == "esc" && m.busyCanCancel {
			m.busyCancel()
			return m, nil
		}
		if ok && m.busyDone && m.busyErr != nil && m.busyRetry != nil && k.String() == "l" {
			return m, m.busyRetry()
		}
		if ok && m.busyDone && (k.String() == "enter" || k.String() == "esc" || k.String() == "q") {
			m.screen = scrDashboard
			if m.afterBusy != nil && m.busyErr == nil {
				return m, m.afterBusy()
			}
			if m.App != nil {
				return m, m.LoadServers()
			}
			if m.busyErr != nil && Wizard != nil {
				return m, Wizard(m)
			}
		}
		return m, nil
	case scrLogs:
		if k, ok := msg.(tea.KeyPressMsg); ok && !m.logs.Editing() && (k.String() == "esc" || k.String() == "q") {
			m.stopLogs()
			m.screen = scrDashboard
			return m, nil
		}
		var cmd tea.Cmd
		m.logs, cmd = m.logs.Update(msg)
		return m, cmd
	case scrAccount:
		return m.updateAccount(msg)
	case scrRuntime:
		return m.updateRuntime(msg)
	case scrUpdates:
		return m.updateUpdates(msg)
	case scrInfo:
		if k, ok := msg.(tea.KeyPressMsg); ok && (k.String() == "esc" || k.String() == "q" || k.String() == "enter") {
			m.screen = scrDashboard
			return m, m.LoadServers()
		}
		return m, nil
	}
	return m.updateDashboard(msg)
}

func (m *Model) refresh() {
	rows := make([]table.Row, 0, len(m.servers))
	for _, s := range m.servers {
		reg, rt, matches, note := "-", "-", "0", ""
		if st := s.Status; st != nil {
			if st.PublicIP != "" {
				reg = fmt.Sprintf("%s:%d", st.PublicIP, st.Port)
			}
			if st.RuntimeVersion != "" {
				rt = st.RuntimeVersion
			}
			matches = strconv.Itoa(st.SessionsTotal)
			if st.LastError != "" && (s.State() == "failed" || s.State() == "backoff") {
				note = st.LastError
			}
		}
		rows = append(rows, table.Row{s.Name(), s.Panel.Identifier, strconv.Itoa(s.Port), ui.StateLabel(s.State()), rt, reg, matches, note})
	}
	m.table.SetRows(rows)
}

func (m *Model) selected() (core.Server, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.servers) {
		return core.Server{}, false
	}
	return m.servers[i], true
}

func (m *Model) ask(q string, f func() tea.Cmd) { m.confirm, m.confirmFunc = q, f }

func (m *Model) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	if m.App == nil {
		if k.String() == "q" {
			return m, tea.Quit
		}
		return m, nil
	}
	app := m.App
	sel, has := m.selected()
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "enter", "l":
		if has {
			return m, m.openLogs(sel)
		}
	case "s", "x", "r", "K":
		if !has {
			return m, nil
		}
		sig := map[string]string{"s": "start", "x": "stop", "r": "restart", "K": "kill"}[k.String()]
		run := func() tea.Cmd {
			return m.quick(sel.Name()+": "+sig+" sent", func(ctx context.Context) error { return app.Power(ctx, sel, sig) })
		}
		if sig == "start" {
			return m, run()
		}
		m.ask(fmt.Sprintf("%s %s? Players in a match are disconnected.", strings.ToUpper(sig[:1])+sig[1:], sel.Name()), run)
	case "a":
		if !app.HasAccount() {
			m.flash, m.flashBad = "Sign in first: l links this host (no password), p uses a password.", true
			return m, m.openAccount()
		}
		return m, m.addForm()
	case "i":
		if has {
			m.ask("Reinstall "+sel.Name()+"? It stops and runs the egg's install step again.", func() tea.Cmd {
				return m.quick(sel.Name()+": reinstalling", func(ctx context.Context) error { return app.Panel().Reinstall(ctx, sel.Panel.ID) })
			})
		}
	case "m":
		return m, m.menu()
	case "U":
		return m, m.openUpdates()
	case "M":
		return m, m.loadLegacy()
	case "e":
		if has {
			return m, m.editForm(sel)
		}
	case "d":
		if has {
			m.ask("Delete "+sel.Name()+" from the panel?", func() tea.Cmd {
				return m.quick(sel.Name()+" deleted", func(ctx context.Context) error { return app.Delete(ctx, sel) })
			})
		}
	case "c":
		return m, m.openAccount()
	case "S":
		return m, m.rerunSetup()
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *Model) openLogs(s core.Server) tea.Cmd {
	m.stopLogs()
	m.screen, m.logName = scrLogs, s.Name()
	m.logs.Reset()
	ctx, cancel := context.WithCancel(m.Ctx)
	m.logCancel = cancel
	app, name := m.App, s.Name()
	ch := make(chan tea.Msg, 256)
	go func() {
		if err := app.Console(ctx, s, func(l string) { ch <- ui.LogLineMsg{Source: name, Line: l} }); err != nil && ctx.Err() == nil {
			ch <- ui.LogLineMsg{Source: name, Line: "[evr] console ended: " + err.Error()}
		}
		close(ch)
	}()
	return waitChan(ch)
}

func (m *Model) stopLogs() {
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
}

func intRange(lo, hi int) func(string) error {
	return func(s string) error {
		if s == "" {
			return nil
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < lo || n > hi {
			return fmt.Errorf("enter a number from %d to %d", lo, hi)
		}
		return nil
	}
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (m *Model) addForm() tea.Cmd {
	count, regions, guilds := "1", strings.Join(m.App.Cfg.Defaults.Regions, ","), strings.Join(m.App.Cfg.Defaults.Guilds, ",")
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("How many servers?").Description("They get free ports on the node and start after installing.").Value(&count).Validate(intRange(1, 32)),
		huh.NewInput().Title("Regions (optional)").Description("Comma-separated, e.g. euw,default.").Value(&regions),
		huh.NewInput().Title("Guilds (optional)").Description("Guild IDs to host for; empty = all the account may host.").Value(&guilds),
	).Title("Add Echo VR servers"))
	app := m.App
	return m.ShowForm(f, func() tea.Cmd {
		n, _ := strconv.Atoi(count)
		set := instance.Settings{Regions: split(regions), Guilds: split(guilds)}
		return m.Run(fmt.Sprintf("Creating %d server(s)", n), func(ctx context.Context, say func(string)) error {
			_, err := app.CreateServers(ctx, n, set, say)
			return err
		}, nil)
	})
}

func (m *Model) editForm(s core.Server) tea.Cmd {
	set := core.SettingsFromEnv(s.Panel.Env())
	regions, guilds, tick, runtime := strings.Join(set.Regions, ","), strings.Join(set.Guilds, ","), strconv.Itoa(set.TickRate), set.Runtime
	ttl, restart := strconv.Itoa(set.EmptyTTL), true
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Regions").Value(&regions),
		huh.NewInput().Title("Guilds").Description("Guild IDs to host for; empty = all the account may host.").Value(&guilds),
		huh.NewInput().Title("Tick rate (Hz)").Value(&tick).Validate(intRange(10, 1000)),
		huh.NewInput().Title("Empty server TTL (seconds)").Description("0 keeps the runtime's default.").Value(&ttl).Validate(intRange(0, 86400)),
		huh.NewInput().Title("nevr-runtime").Description(`"current" follows the node; or a tag like v5.4.2.`).Value(&runtime),
		huh.NewConfirm().Title("Restart now if it is running?").Description("Otherwise the change applies at its next restart.").Value(&restart),
	).Title("Edit " + s.Name()))
	app := m.App
	return m.ShowForm(f, func() tea.Cmd {
		return m.Run("Updating "+s.Name(), func(ctx context.Context, say func(string)) error {
			err := app.UpdateServer(ctx, s, func(cur *instance.Settings) {
				cur.Regions, cur.Guilds = split(regions), split(guilds)
				cur.TickRate, _ = strconv.Atoi(tick)
				cur.EmptyTTL, _ = strconv.Atoi(ttl)
				cur.Runtime = runtime
			}, restart)
			if err == nil && restart {
				say("saved; the server restarts if it was running")
			} else if err == nil {
				say("saved; applies at the next restart")
			}
			return err
		}, nil)
	})
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *Model) header() string {
	t := ui.Title.Render("evr-ptero")
	if m.App == nil {
		return t + "  " + ui.Subtle.Render("Echo VR servers on Pterodactyl / Pelican")
	}
	p := m.App.Cfg.Panel
	return t + "  " + ui.Subtle.Render(fmt.Sprintf("%s (%s) · node #%d · runtime %s", p.URL, p.Kind, p.NodeID, orDash(m.App.Store().Current())))
}

// wrap breaks s to the terminal width minus indent.
func (m *Model) wrap(s string, indent int) string {
	w := m.width - indent
	if w < 20 {
		w = 76
	}
	return lipgloss.NewStyle().Width(w).Render(s)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (m *Model) footer(keys ...ui.Key) string {
	line := "\n"
	switch {
	case m.confirm != "":
		line = ui.Warning.Render(m.confirm) + " " + ui.KeyStyle.Render("[y/N]") + "\n"
	case m.flash != "" && m.flashBad:
		line = ui.Error.Render(m.flash) + "\n"
	case m.flash != "":
		line = ui.Success.Render(m.flash) + "\n"
	}
	return line + ui.HelpBar(keys...)
}

func (m *Model) render() string {
	switch m.screen {
	case scrForm:
		return m.header() + "\n\n" + m.form.View()
	case scrBusy:
		var b strings.Builder
		cancelled := errors.Is(m.busyErr, context.Canceled)
		head := m.spin.View() + " " + ui.Bold.Render(m.busyTitle)
		switch {
		case m.busyDone && cancelled:
			head = ui.Warning.Render("✘ " + m.busyTitle + ": cancelled")
		case m.busyDone && m.busyErr != nil:
			head = ui.Error.Render("✘ " + m.busyTitle + " failed")
		case m.busyDone:
			head = ui.Success.Render("✔ " + m.busyTitle)
		}
		b.WriteString(m.header() + "\n\n" + head + "\n\n")
		var lines []string
		for _, l := range m.busyLines {
			lines = append(lines, strings.Split(m.wrap(l, 4), "\n")...)
		}
		if n := m.height - 9; n > 0 && len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		for _, l := range lines {
			b.WriteString("  " + l + "\n")
		}
		if m.busyErr != nil && !cancelled {
			b.WriteString("\n" + ui.Error.Render(m.wrap(m.busyErr.Error(), 0)) + "\n")
		}
		switch {
		case m.busyDone && m.busyErr != nil && m.busyRetry != nil:
			b.WriteString("\n" + ui.HelpBar(ui.K("l", "link again"), ui.K("enter", "continue")))
		case m.busyDone:
			b.WriteString("\n" + ui.HelpBar(ui.K("enter", "continue")))
		case m.busyCanCancel:
			b.WriteString("\n" + ui.HelpBar(ui.K("esc", "cancel")))
		}
		return b.String()
	case scrLogs:
		return m.header() + "  " + ui.Bold.Render("console: "+m.logName) + "\n" + m.logs.View() + "\n" +
			ui.HelpBar(ui.K("esc", "back"), ui.K("f", "follow"), ui.K("a", "all lines"), ui.K("/", "filter"))
	case scrAccount:
		return m.renderAccount()
	case scrRuntime:
		return m.renderRuntime()
	case scrUpdates:
		return m.renderUpdates()
	case scrInfo:
		return m.header() + "  " + ui.Bold.Render(m.infoTitle) + "\n\n" + m.infoText + "\n" + ui.HelpBar(ui.K("esc", "back"))
	}
	if m.App == nil {
		msg := "Not set up yet."
		if m.loadErr != nil {
			msg = ui.Error.Render(m.loadErr.Error())
		}
		return m.header() + "\n\n" + msg + "\n\n" + ui.HelpBar(ui.K("q", "quit"))
	}
	body := m.table.View()
	if len(m.servers) == 0 {
		body = ui.Subtle.Render("No Echo VR servers in the panel yet. Press ") + ui.KeyStyle.Render("a") + ui.Subtle.Render(" to create some.")
	}
	if m.loadErr != nil {
		body += "\n" + ui.Error.Render(m.loadErr.Error())
	}
	body += m.updatesNotice()
	if n := len(m.legacy); n > 0 {
		body += "\n" + ui.Warning.Render(fmt.Sprintf("%d server(s) still on an old Echo egg: press ", n)) + ui.KeyStyle.Render("M") + ui.Warning.Render(" to move them (no reinstall)")
	}
	if !m.App.HasAccount() {
		body += "\n" + ui.Warning.Render("The servers have no EchoVRCE sign-in yet: press ") + ui.KeyStyle.Render("c") + ui.Warning.Render(" to link this host.")
	}
	return m.header() + "\n\n" + body + "\n" + m.footer(
		ui.K("enter", "console"), ui.K("s", "start"), ui.K("x", "stop"), ui.K("r", "restart"), ui.K("K", "kill"),
		ui.K("a", "add"), ui.K("e", "edit"), ui.K("i", "reinstall"), ui.K("d", "delete"), ui.K("c", "account"), ui.K("U", "updates"), ui.K("m", "more"), ui.K("q", "quit"))
}
