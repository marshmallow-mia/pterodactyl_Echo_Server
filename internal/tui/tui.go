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
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/auth"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/supervisor"
	ui "github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/tui"
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
	return err
}

type screen int

const (
	scrDashboard screen = iota
	scrLogs
	scrForm
	scrBusy
	scrAccount
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
	spin      spinner.Model

	account *accountInfo

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
	ch := make(chan tea.Msg, 128)
	go func() {
		err := task(m.Ctx, func(s string) { ch <- busyLineMsg{s} })
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
		return m, tick()
	case busyLineMsg:
		m.busyLines = append(m.busyLines, msg.line)
		return m, nil
	case busyDoneMsg:
		m.busyDone, m.busyErr = true, msg.err
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
		if k, ok := msg.(tea.KeyPressMsg); ok && m.busyDone && (k.String() == "enter" || k.String() == "esc" || k.String() == "q") {
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
		if k, ok := msg.(tea.KeyPressMsg); ok {
			switch k.String() {
			case "esc", "q":
				m.screen = scrDashboard
			case "r":
				m.account = nil
				return m, m.loadAccount()
			}
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
		return m, m.addForm()
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
		m.screen, m.account = scrAccount, nil
		return m, m.loadAccount()
	case "S":
		return m, m.Run("Re-running panel setup", func(ctx context.Context, say func(string)) error {
			_, err := core.Setup(ctx, m.Dir, core.SetupOptions{}, func(step, detail string) { say(step + ": " + detail) })
			return err
		}, nil)
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
	count, regions := "1", strings.Join(m.App.Cfg.Defaults.Regions, ",")
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("How many servers?").Description("They get free ports on the node and start after installing.").Value(&count).Validate(intRange(1, 32)),
		huh.NewInput().Title("Regions (optional)").Value(&regions),
	).Title("Add Echo VR servers"))
	app := m.App
	return m.ShowForm(f, func() tea.Cmd {
		n, _ := strconv.Atoi(count)
		set := instance.Settings{Regions: split(regions)}
		return m.Run(fmt.Sprintf("Creating %d server(s)", n), func(ctx context.Context, say func(string)) error {
			_, err := app.CreateServers(ctx, n, set, say)
			return err
		}, nil)
	})
}

func (m *Model) editForm(s core.Server) tea.Cmd {
	set := core.SettingsFromEnv(s.Panel.Env())
	regions, guilds, tick, runtime := strings.Join(set.Regions, ","), strings.Join(set.Guilds, ","), strconv.Itoa(set.TickRate), set.Runtime
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Regions").Value(&regions),
		huh.NewInput().Title("Guilds").Description("Guild IDs to host for; empty = all the account may host.").Value(&guilds),
		huh.NewInput().Title("Tick rate (Hz)").Value(&tick).Validate(intRange(10, 1000)),
		huh.NewInput().Title("nevr-runtime").Description(`"current" follows the node; or a tag like v5.4.2.`).Value(&runtime),
	).Title("Edit " + s.Name()))
	app := m.App
	return m.ShowForm(f, func() tea.Cmd {
		return m.Run("Updating "+s.Name(), func(ctx context.Context, say func(string)) error {
			err := app.UpdateServer(ctx, s, func(cur *instance.Settings) {
				cur.Regions, cur.Guilds = split(regions), split(guilds)
				cur.TickRate, _ = strconv.Atoi(tick)
				cur.Runtime = runtime
			}, true)
			if err == nil {
				say("saved; the server restarts if it was running")
			}
			return err
		}, nil)
	})
}

func (m *Model) loadAccount() tea.Cmd {
	app := m.App
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.Ctx, 20*time.Second)
		defer cancel()
		info := &accountInfo{}
		info.session, info.err = app.Login(ctx)
		if info.err == nil {
			info.servers, _ = app.Auth().MyServers(ctx, info.session)
		}
		return accountMsg(info)
	}
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
		head := m.spin.View() + " " + ui.Bold.Render(m.busyTitle)
		if m.busyDone && m.busyErr != nil {
			head = ui.Error.Render("✘ " + m.busyTitle + " failed")
		} else if m.busyDone {
			head = ui.Success.Render("✔ " + m.busyTitle)
		}
		b.WriteString(m.header() + "\n\n" + head + "\n\n")
		lines := m.busyLines
		if n := m.height - 9; n > 0 && len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		for _, l := range lines {
			b.WriteString("  " + ui.Truncate(l, m.width-4) + "\n")
		}
		if m.busyErr != nil {
			b.WriteString("\n" + ui.Error.Render(m.busyErr.Error()) + "\n")
		}
		if m.busyDone {
			b.WriteString("\n" + ui.HelpBar(ui.K("enter", "continue")))
		}
		return b.String()
	case scrLogs:
		return m.header() + "  " + ui.Bold.Render("console: "+m.logName) + "\n" + m.logs.View() + "\n" +
			ui.HelpBar(ui.K("esc", "back"), ui.K("f", "follow"), ui.K("a", "all lines"), ui.K("/", "filter"))
	case scrAccount:
		return m.renderAccount()
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
	return m.header() + "\n\n" + body + "\n" + m.footer(
		ui.K("enter", "console"), ui.K("s", "start"), ui.K("x", "stop"), ui.K("r", "restart"), ui.K("K", "kill"),
		ui.K("a", "add"), ui.K("e", "edit"), ui.K("d", "delete"), ui.K("c", "account"), ui.K("S", "re-run setup"), ui.K("q", "quit"))
}

func (m *Model) renderAccount() string {
	var b strings.Builder
	b.WriteString(m.header() + "  " + ui.Bold.Render("account") + "\n\n")
	a := m.account
	switch {
	case a == nil:
		b.WriteString(m.spin.View() + " signing in …\n")
	case a.err != nil:
		b.WriteString(ui.Error.Render(a.err.Error()) + "\n")
	default:
		fmt.Fprintf(&b, "Signed in as %s; %s\n\n", ui.Bold.Render(a.session.Username), a.session.HostSummary())
		for _, g := range a.session.HostGuilds() {
			b.WriteString("  " + g + "\n")
		}
		fmt.Fprintf(&b, "\nThe backend lists %d registered server(s):\n", len(a.servers))
		for _, s := range a.servers {
			fmt.Fprintf(&b, "  %s  %-12s %s\n", s.CreatedAt.Local().Format(time.DateTime), s.Region, s.LobbyType)
		}
	}
	b.WriteString("\n" + ui.Subtle.Render("Change the account with: evr-ptero account set --apply") + "\n")
	return b.String() + m.footer(ui.K("r", "refresh"), ui.K("esc", "back"))
}
