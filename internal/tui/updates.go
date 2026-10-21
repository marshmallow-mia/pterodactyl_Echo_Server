package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	ui "github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/tui"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/updates"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/agent"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panelinstall"
)

type (
	updatesMsg  struct{}
	versionsMsg struct {
		c       updates.Component
		vs      []string
		switch_ map[string]bool
		err     error
	}
)

// openUpdates shows the last results at once and checks again in the background.
func (m *Model) openUpdates() tea.Cmd {
	m.screen = scrUpdates
	m.refreshUpdates()
	return m.checkUpdates()
}

func (m *Model) checkUpdates() tea.Cmd {
	app := m.App
	m.updChecking = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.Ctx, 3*time.Minute)
		defer cancel()
		updates.CheckAll(ctx, app.Dir, agent.Components(app, nil), app.Cfg.Updates.Settings())
		return updatesMsg{}
	}
}

func (m *Model) refreshUpdates() {
	rec := updates.LoadRecord(m.App.Dir)
	set := m.App.Cfg.Updates.Settings()
	m.updComps = agent.Components(m.App, nil)
	if m.width > 0 {
		m.updTable.SetWidth(m.width)
	}
	rows := make([]table.Row, 0, len(m.updComps))
	for _, c := range m.updComps {
		mode := set.Mode(c.Name())
		inst, avail, note := "-", "", ""
		if st, ok := rec.Get(c.Name()); ok {
			inst = st.Installed
			switch {
			case st.Error != "":
				avail, note = "?", "check failed: "+st.Error
			case st.Available:
				avail = "● " + st.Latest
			default:
				avail = "up to date"
			}
			if note == "" {
				note = "checked " + st.Checked.Local().Format("Jan 2 15:04")
			}
		} else if mode == updates.Off {
			note = "not checked (off)"
		}
		rows = append(rows, table.Row{c.Title(), inst, avail, string(mode), note})
	}
	m.updTable.SetRows(rows)
}

func (m *Model) updateUpdates(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		m.updTable, cmd = m.updTable.Update(msg)
		return m, cmd
	}
	switch k.String() {
	case "esc", "q":
		m.screen = scrDashboard
		return m, m.LoadServers()
	case "c":
		return m, m.checkUpdates()
	case "s":
		return m, m.updateSettingsForm()
	case "enter":
		if i := m.updTable.Cursor(); i >= 0 && i < len(m.updComps) {
			return m, m.loadVersions(m.updComps[i])
		}
	}
	var cmd tea.Cmd
	m.updTable, cmd = m.updTable.Update(msg)
	return m, cmd
}

// loadVersions fetches what c can be updated to, then opens the form.
func (m *Model) loadVersions(c updates.Component) tea.Cmd {
	m.flash, m.flashBad = "looking up versions of "+c.Title()+" …", false
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.Ctx, time.Minute)
		defer cancel()
		vs, err := c.Versions(ctx)
		sw := map[string]bool{}
		if p, ok := c.(*agent.Panel); ok {
			for _, v := range vs {
				sw[v] = p.SwitchesChannel(ctx, v)
			}
		}
		return versionsMsg{c, vs, sw, err}
	}
}

var updateWhat = map[string]string{
	"self":    "evr-ptero is replaced (checksum-verified, the old one kept as .prev) and restarts; the egg and the servers move to the new image (each server at its next restart).",
	"game":    "Missing or changed files are fetched from the mirror and verified; servers use them from their next restart.",
	"runtime": "The version is tried on one idle server first; only if it registers do the others switch, at their next match boundary.",
	"panel":   "The panel and its database are backed up to " + panelinstall.BackupDir + ", the panel goes into maintenance mode for a minute or two, then the new version is installed and migrated. If it does not answer afterwards, the backup is restored.",
	"wings":   "Wings is replaced and restarted; game servers keep running. If it does not come back, the old version is put back.",
}

// applyForm asks which version to install, then installs it.
func (m *Model) applyForm(msg versionsMsg) tea.Cmd {
	c, vs := msg.c, msg.vs
	if msg.err != nil {
		m.flash, m.flashBad = "versions of "+c.Title()+": "+msg.err.Error(), true
		return nil
	}
	m.flash = ""
	st, _ := updates.LoadRecord(m.App.Dir).Get(c.Name())
	version, ok, typed := "", false, ""
	fields := []huh.Field{huh.NewNote().Title(c.Title()).Description(fmt.Sprintf("Installed: %s\nAvailable: %s", st.Installed, orDash(st.Latest)))}
	if len(vs) > 0 {
		opts := []huh.Option[string]{huh.NewOption("the newest of the update channel ("+orDash(st.Latest)+")", "")}
		for _, v := range vs {
			label := v
			if strings.TrimPrefix(strings.Fields(v)[0], "v") == strings.TrimPrefix(st.Installed, "v") {
				label += " (installed: reinstall)"
			}
			opts = append(opts, huh.NewOption(label, v))
		}
		fields = append(fields, huh.NewSelect[string]().Title("Version").Options(opts...).Value(&version).Height(8))
	}
	groups := []*huh.Group{huh.NewGroup(fields...).Title("Update")}
	groups = append(groups, huh.NewGroup(
		huh.NewNote().Title("Another channel").Description(ui.Warning.Render("This version is in another channel than the installed panel. "+
			"Its database migration only goes forward: going back means restoring the backup.")),
		huh.NewInput().Title("Type the version to confirm").Value(&typed).Validate(func(s string) error {
			if strings.TrimSpace(s) != strings.Fields(version)[0] {
				return errors.New("type " + strings.Fields(version)[0] + ", or press esc")
			}
			return nil
		}),
	).WithHideFunc(func() bool { return !msg.switch_[version] }))
	groups = append(groups, huh.NewGroup(huh.NewConfirm().Title("Install now?").Description(updateWhat[c.Name()]).Value(&ok)))
	return m.ShowForm(huh.NewForm(groups...), func() tea.Cmd {
		if !ok {
			return nil
		}
		app := m.App
		return m.Run("Updating "+c.Title(), func(ctx context.Context, say func(string)) error {
			comp := c
			for _, x := range agent.Components(app, say) { // with progress output
				if x.Name() == c.Name() {
					comp = x
				}
			}
			if err := comp.Apply(ctx, version, say); err != nil {
				return err
			}
			updates.CheckOne(ctx, app.Dir, comp)
			return nil
		}, func() tea.Cmd {
			if c.Name() == "self" {
				m.reexec = true
				return tea.Quit
			}
			m.screen = scrUpdates
			m.refreshUpdates()
			return nil
		})
	})
}

func (m *Model) updateSettingsForm() tea.Cmd {
	app := m.App
	u := app.Cfg.Updates
	hours := fmt.Sprint(app.Cfg.Updates.Settings().Interval() / time.Hour)
	mode := func(title, desc string, v *string) huh.Field {
		return huh.NewSelect[string]().Title(title).Description(desc).Options(
			huh.NewOption("notify: check, show here and send an alert", "notify"),
			huh.NewOption("auto: install automatically", "auto"),
			huh.NewOption("off: do not check", "off"),
		).Value(v)
	}
	fields := []huh.Field{
		mode("evr-ptero itself", "", &u.Self),
		mode("Game files", "Repairs missing or changed files from the mirror.", &u.Game),
		mode("nevr-runtime", "Tried on one idle server first, then rolled out at match boundaries.", &u.Runtime),
	}
	var panelFields []huh.Field
	for _, c := range agent.Components(app, nil) {
		switch c.Name() {
		case "panel":
			panelFields = append(panelFields, mode(c.Title(), "Backed up first; restored automatically if the update fails.", &u.Panel),
				huh.NewSelect[string]().Title("Panel releases to follow").Options(
					huh.NewOption("the installed channel", ""),
					huh.NewOption("stable: full Pterodactyl 1.x releases", "stable"),
					huh.NewOption("beta: Pterodactyl 2.x (one-way migration from 1.x)", "beta"),
				).Value(&u.PanelChannel))
		case "wings":
			panelFields = append(panelFields, mode("Wings", "Restarted with the new version; game servers keep running.", &u.Wings))
		}
	}
	groups := []*huh.Group{huh.NewGroup(fields...).Title("How should each part be updated?")}
	if len(panelFields) > 0 {
		groups = append(groups, huh.NewGroup(panelFields...).Title("Panel and Wings on this host"))
	}
	groups = append(groups, huh.NewGroup(
		huh.NewInput().Title("Check every … hours").Value(&hours).Validate(intRange(1, 168)),
		huh.NewInput().Title("Install automatic updates only between").Description("Local time, e.g. 04:00-06:00. Empty: any time.").Value(&u.Window).Validate(updates.ValidWindow),
		huh.NewNote().Title("Background service").DescriptionFunc(func() string {
			if agent.ServiceInstalled() {
				return "Running: it checks and installs as set here."
			}
			return "Not running: nothing is checked or installed in the background. Turn it on under More → Discord alerts and the background service."
		}, nil),
	))
	return m.ShowForm(huh.NewForm(groups...), func() tea.Cmd {
		fmt.Sscan(hours, &u.CheckHours)
		app.Cfg.Updates = u
		if err := app.Save(); err != nil {
			m.flash, m.flashBad = err.Error(), true
		} else {
			m.flash, m.flashBad = "update settings saved", false
		}
		m.screen = scrUpdates
		m.refreshUpdates()
		return nil
	})
}

func (m *Model) renderUpdates() string {
	status := ""
	if m.updChecking {
		status = m.spin.View() + " checking …"
	} else if rec := updates.LoadRecord(m.App.Dir); !rec.Checked.IsZero() {
		status = ui.Subtle.Render("last check " + rec.Checked.Local().Format("Jan 2 15:04"))
	}
	auto := ui.Subtle.Render("Automatic updates run in the background service.")
	if !agent.ServiceInstalled() {
		auto = ui.Warning.Render("The background service is off: nothing is checked or installed automatically (More → Discord alerts and the background service).")
	}
	return m.header() + "  " + ui.Bold.Render("updates") + "\n\n" + m.updTable.View() + "\n\n" + status + "\n" + m.wrap(auto, 0) + "\n" +
		m.footer(ui.K("enter", "update"), ui.K("c", "check now"), ui.K("s", "settings"), ui.K("esc", "back"))
}

// updatesNotice is the dashboard line when updates are waiting.
func (m *Model) updatesNotice() string {
	avail := updates.LoadRecord(m.App.Dir).Available()
	if len(avail) == 0 {
		return ""
	}
	return "\n" + ui.Warning.Render(fmt.Sprintf("%d update(s) available: press ", len(avail))) + ui.KeyStyle.Render("U")
}

// reexecIfUpdated starts the new binary after a self-update.
func reexecIfUpdated(m *Model) error {
	if !m.reexec {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
