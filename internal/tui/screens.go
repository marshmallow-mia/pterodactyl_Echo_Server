package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	ui "github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/tui"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/update"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/agent"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/egg"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panelinstall"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// linkFailed marks a failed device link, so the busy screen offers to retry.
type linkFailed struct{ error }

func (e linkFailed) Unwrap() error { return e.error }

type (
	runtimesMsg struct {
		list []nevr.Installed
		err  error
	}
	legacyMsg struct {
		list []core.LegacyServer
		err  error
	}
)

// Account screen.

func (m *Model) openAccount() tea.Cmd {
	m.screen, m.account = scrAccount, nil
	return m.loadAccount()
}

func (m *Model) loadAccount() tea.Cmd {
	app := m.App
	return func() tea.Msg {
		info := &accountInfo{}
		if !app.HasAccount() {
			info.err = core.ErrNoAccount
			return accountMsg(info)
		}
		ctx, cancel := context.WithTimeout(m.Ctx, 20*time.Second)
		defer cancel()
		info.session, info.err = app.Login(ctx)
		if info.err == nil {
			info.servers, _ = app.Auth().MyServers(ctx, info.session)
		}
		return accountMsg(info)
	}
}

func (m *Model) updateAccount(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		m.screen = scrDashboard
		return m, m.LoadServers()
	case "r":
		return m, m.openAccount()
	case "l":
		return m, m.linkDevice()
	case "p":
		return m, m.passwordForm()
	}
	return m, nil
}

// linkDevice links this host at echovrce.com/device and gives every server the link.
func (m *Model) linkDevice() tea.Cmd {
	app := m.App
	cmd := m.Run("Linking this host at echovrce.com/device", func(ctx context.Context, say func(string)) error {
		say(core.ResetPasswordNote)
		say("")
		_, err := app.UseDeviceLink(ctx, func(url, code string, expires time.Time) {
			say("Open " + url)
			say("and approve the code " + code + " (valid until " + expires.Local().Format(time.TimeOnly) + ")")
			say("waiting …")
		}, say)
		if err != nil {
			return linkFailed{err}
		}
		return nil
	}, func() tea.Cmd { return m.openAccount() })
	m.busyCanCancel = true
	return cmd
}

func (m *Model) passwordForm() tea.Cmd {
	app := m.App
	discordID, password := app.Secrets[config.EnvDiscordID], ""
	f := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Password sign-in").Description("Use this if your EchoVRCE account has a server password.\n"+
			"Without one, go back and press l to link this host instead."),
		huh.NewInput().Title("Discord user ID").Description("Discord → Settings → Advanced → Developer mode, then copy your ID.").Value(&discordID).Validate(digits),
		huh.NewInput().Title("Server password").Description("The password you set for hosting with the EchoVRCE bot.").EchoMode(huh.EchoModePassword).Value(&password).Validate(required("the password")),
	).Title("EchoVRCE account"))
	return m.ShowForm(f, func() tea.Cmd {
		return m.Run("Signing in", func(ctx context.Context, say func(string)) error {
			_, err := app.UsePassword(ctx, discordID, password, say)
			return err
		}, func() tea.Cmd { return m.openAccount() })
	})
}

func (m *Model) renderAccount() string {
	var b strings.Builder
	b.WriteString(m.header() + "  " + ui.Bold.Render("account") + "\n\n")
	a := m.account
	switch {
	case a == nil:
		b.WriteString(m.spin.View() + " signing in …\n")
	case errors.Is(a.err, core.ErrNoAccount):
		b.WriteString(ui.Warning.Render("Not signed in yet: the servers cannot register with EchoVRCE.") + "\n\n")
		b.WriteString("Press " + ui.KeyStyle.Render("l") + " to link this host at echovrce.com/device (recommended, no password).\n")
		b.WriteString(ui.Subtle.Render(m.wrap(core.ResetPasswordNote, 0)) + "\n\n")
		b.WriteString("Or press " + ui.KeyStyle.Render("p") + " to sign in with your Discord ID and server password.\n")
	case a.err != nil:
		b.WriteString(ui.Error.Render(m.wrap(a.err.Error(), 0)) + "\n\n")
		b.WriteString(ui.Subtle.Render("Press l to link this host again, or p to use a password.") + "\n")
	default:
		method := "device link"
		if m.App.Secrets[config.EnvPassword] != "" {
			method = "password"
		}
		fmt.Fprintf(&b, "Signed in as %s (Discord ID %s, %s); %s\n\n", ui.Bold.Render(a.session.Username), m.App.Secrets[config.EnvDiscordID], method, a.session.HostSummary())
		for _, g := range a.session.HostGuilds() {
			b.WriteString("  " + g + "\n")
		}
		fmt.Fprintf(&b, "\nThe backend lists %d registered server(s):\n", len(a.servers))
		for _, s := range a.servers {
			fmt.Fprintf(&b, "  %s  %-12s %-11s players %d\n", s.CreatedAt.Local().Format(time.DateTime), s.Region, s.LobbyType, s.Players)
		}
	}
	return b.String() + m.footer(ui.K("l", "link this host"), ui.K("p", "password sign-in"), ui.K("r", "refresh"), ui.K("esc", "back"))
}

// The "more" menu.

func (m *Model) menu() tea.Cmd {
	choice := "runtime"
	opts := []huh.Option[string]{
		huh.NewOption("Runtime versions and updates", "runtime"),
		huh.NewOption("Discord alerts and the background service", "alerts"),
		huh.NewOption("EchoVRCE account", "account"),
		huh.NewOption("Move servers from an old Echo egg", "migrate"),
		huh.NewOption("Panel connection", "panel"),
		huh.NewOption("Re-run the panel setup (egg, mounts, allocations, game files)", "setup"),
		huh.NewOption("Write the egg files", "egg"),
	}
	if m.App.Cfg.Panel.Local {
		opts = append(opts, huh.NewOption("Uninstall the panel from this host", "uninstall"))
	}
	f := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("What do you want to do?").Options(opts...).Value(&choice)).Title("More"))
	return m.ShowForm(f, func() tea.Cmd {
		switch choice {
		case "runtime":
			return m.openRuntime()
		case "alerts":
			return m.alertsForm()
		case "account":
			return m.openAccount()
		case "migrate":
			return m.loadLegacy()
		case "panel":
			m.showPanelInfo()
			return nil
		case "setup":
			return m.rerunSetup()
		case "egg":
			return m.writeEggs()
		case "uninstall":
			return m.uninstallForm()
		}
		return nil
	})
}

func (m *Model) rerunSetup() tea.Cmd {
	return m.Run("Re-running panel setup", func(ctx context.Context, say func(string)) error {
		app, err := core.Setup(ctx, m.Dir, core.SetupOptions{}, func(step, detail string) { say(step + ": " + detail) })
		if err == nil {
			m.App = app
		}
		return err
	}, nil)
}

func (m *Model) showPanelInfo() {
	c, cfg := m.App.Cfg.Panel, m.App.Cfg
	var b strings.Builder
	row := func(k, v string) { fmt.Fprintf(&b, "  %-14s %s\n", k, v) }
	row("Panel", fmt.Sprintf("%s (%s)", c.URL, c.Kind))
	if c.APIURL != "" {
		row("API from here", c.APIURL)
	}
	row("On this host", fmt.Sprint(c.Local))
	row("Node", fmt.Sprintf("#%d", c.NodeID))
	row("Egg", fmt.Sprintf("#%d (nest %q)", c.EggID, c.Nest))
	row("Mounts", fmt.Sprint(c.MountIDs))
	row("Owner", fmt.Sprintf("user #%d", c.OwnerID))
	row("Game ports", fmt.Sprintf("on %s from %d (alias %s)", orDash(c.BindIP), pcfg.FirstPort, orDash(c.Alias)))
	row("Shared files", fmt.Sprintf("%s (%s)", cfg.DataDir, cfg.GameSource))
	row("Image", cfg.Image)
	row("Config", filepath.Join(m.Dir, "config.yaml"))
	if _, err := os.Stat(filepath.Join(m.Dir, "install.env")); err == nil {
		row("Panel login", filepath.Join(m.Dir, "install.env")+" (from the install)")
	}
	if m.App.Secrets[pcfg.SecretClientKey] == "" {
		b.WriteString("\n" + ui.Warning.Render("No admin account API key: servers cannot be started or show their console from here.") + "\n")
	}
	m.screen, m.infoTitle, m.infoText = scrInfo, "panel", b.String()
}

func (m *Model) writeEggs() tea.Cmd {
	image := m.App.Cfg.Image
	return m.Run("Writing the egg files", func(ctx context.Context, say func(string)) error {
		return writeEggFiles(image, say)
	}, nil)
}

// writeEggFiles writes both egg formats into the working directory.
func writeEggFiles(image string, say func(string)) error {
	wd, _ := os.Getwd()
	for _, fm := range []string{"ptdl", "plcn"} {
		b, err := egg.Render(fm, egg.Spec{Image: image, Version: core.Version})
		if err != nil {
			return err
		}
		name := map[string]string{"ptdl": "egg-echovr-nevr.json", "plcn": "egg-echovr-nevr.yaml"}[fm]
		if err := os.WriteFile(filepath.Join(wd, name), append(b, '\n'), 0o644); err != nil {
			return err
		}
		say("wrote " + filepath.Join(wd, name))
	}
	say("Import the .json in Pterodactyl (Admin → Nests → Import Egg) or the .yaml in Pelican (Eggs → Import).")
	return nil
}

// Runtime screen.

func (m *Model) openRuntime() tea.Cmd {
	m.screen, m.runtimes = scrRuntime, nil
	return m.loadRuntimes()
}

func (m *Model) loadRuntimes() tea.Cmd {
	st := m.App.Store()
	return func() tea.Msg {
		list, err := st.List()
		return runtimesMsg{list, err}
	}
}

func (m *Model) refreshRuntimes() {
	st := m.App.Store()
	cur, prev := st.Current(), st.Previous()
	rows := make([]table.Row, 0, len(m.runtimes))
	for _, r := range m.runtimes {
		state := "installed"
		switch r.Tag {
		case cur:
			state = "● current"
		case prev:
			state = "previous"
		}
		note := ""
		if r.Bad != "" {
			note = "rollout failed: " + r.Bad
		}
		rows = append(rows, table.Row{r.Tag, state, r.InstalledAt.Local().Format(time.DateOnly), note})
	}
	m.rtTable.SetRows(rows)
}

func (m *Model) policy() update.Policy {
	return update.Policy{Channel: m.App.Cfg.Runtime.Channel, Pinned: m.App.Cfg.Runtime.Pinned}
}

func (m *Model) updateRuntime(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		m.rtTable, cmd = m.rtTable.Update(msg)
		return m, cmd
	}
	app := m.App
	var sel *nevr.Installed
	if i := m.rtTable.Cursor(); i >= 0 && i < len(m.runtimes) {
		sel = &m.runtimes[i]
	}
	reload := func() tea.Cmd { m.screen = scrRuntime; return m.loadRuntimes() }
	switch k.String() {
	case "esc", "q":
		m.screen = scrDashboard
		return m, m.LoadServers()
	case "enter":
		if sel == nil {
			return m, nil
		}
		tag := sel.Tag
		m.ask("Make "+tag+" the node's runtime? Servers switch at their next match boundary.", func() tea.Cmd {
			if err := app.Store().Use(tag); err != nil {
				m.flash, m.flashBad = err.Error(), true
			} else {
				m.flash, m.flashBad = tag+" is now current", false
			}
			return m.loadRuntimes()
		})
	case "b":
		tag, err := app.Store().Rollback()
		if err != nil {
			m.flash, m.flashBad = err.Error(), true
		} else {
			m.flash, m.flashBad = "rolled back to "+tag, false
		}
		return m, m.loadRuntimes()
	case "x":
		if sel == nil {
			return m, nil
		}
		tag := sel.Tag
		m.ask("Delete "+tag+" from the shared files?", func() tea.Cmd {
			if err := app.Store().Remove(tag); err != nil {
				m.flash, m.flashBad = err.Error(), true
			} else {
				m.flash, m.flashBad = tag+" deleted", false
			}
			return m.loadRuntimes()
		})
	case "u":
		pol := m.policy()
		return m, m.Run("Checking for a newer nevr-runtime", func(ctx context.Context, say func(string)) error {
			cur, target, available, err := agent.Updater(app, nil).Check(ctx, pol)
			if err != nil {
				return err
			}
			if !available {
				say("up to date (" + cur + ")")
				return nil
			}
			say(fmt.Sprintf("%s is available (current %s): trying it on an idle server first", target.Tag, cur))
			res, err := agent.Updater(app, say).Run(ctx, pol)
			if err != nil {
				return err
			}
			say(fmt.Sprintf("%s: %s → %s %s", res.Action, res.From, res.To, res.Detail))
			return nil
		}, reload)
	case "c":
		return m, m.channelForm()
	}
	var cmd tea.Cmd
	m.rtTable, cmd = m.rtTable.Update(msg)
	return m, cmd
}

func (m *Model) channelForm() tea.Cmd {
	app := m.App
	channel, pinned, auto := app.Cfg.Runtime.Channel, app.Cfg.Runtime.Pinned, app.Cfg.Runtime.AutoUpdate
	f := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Which versions should updates follow?").Options(
				huh.NewOption("pre: newest release incl. pre-releases (every 5.x release is one)", "pre"),
				huh.NewOption("stable: full releases only", "stable"),
				huh.NewOption("pinned: stay on one version", "pinned"),
			).Value(&channel),
			huh.NewConfirm().Title("Update automatically?").Description("Needs the background service (More → Discord alerts and the background service).").Value(&auto),
		).Title("Runtime updates"),
		huh.NewGroup(
			huh.NewInput().Title("Pinned version").Description("e.g. v5.4.2").Value(&pinned).Validate(required("the version")),
		).WithHideFunc(func() bool { return channel != "pinned" }),
	)
	return m.ShowForm(f, func() tea.Cmd {
		app.Cfg.Runtime.Channel, app.Cfg.Runtime.AutoUpdate = channel, auto
		app.Cfg.Runtime.Pinned = ""
		if channel == "pinned" {
			app.Cfg.Runtime.Pinned = pinned
		}
		if err := app.Save(); err != nil {
			m.flash, m.flashBad = err.Error(), true
		} else {
			m.flash, m.flashBad = "runtime updates: "+channel, false
		}
		m.screen = scrRuntime
		return m.loadRuntimes()
	})
}

func (m *Model) renderRuntime() string {
	cfg := m.App.Cfg.Runtime
	policy := "Updates follow the " + cfg.Channel + " channel"
	if cfg.Channel == "pinned" {
		policy = "Pinned to " + cfg.Pinned
	}
	switch {
	case cfg.AutoUpdate && agent.ServiceInstalled():
		policy += "; the background service rolls them out automatically."
	case cfg.AutoUpdate:
		policy += "; automatic updates need the background service (More → Discord alerts and the background service)."
	default:
		policy += "; automatic updates are off."
	}
	body := m.rtTable.View()
	switch {
	case m.runtimes == nil:
		body = m.spin.View() + " loading …"
	case len(m.runtimes) == 0:
		body = ui.Subtle.Render("No runtime installed yet: re-run the panel setup (More) to install the shared files.")
	}
	return m.header() + "  " + ui.Bold.Render("nevr-runtime") + "\n\n" + body + "\n\n" + ui.Subtle.Render(m.wrap(policy, 0)) + "\n" +
		ui.Subtle.Render("Servers pick up a new current version at their next match boundary, so no match is cut short.") + "\n" +
		m.footer(ui.K("enter", "use"), ui.K("b", "rollback"), ui.K("x", "delete"), ui.K("u", "check for updates"), ui.K("c", "update settings"), ui.K("esc", "back"))
}

// Alerts and the background service.

func (m *Model) alertsForm() tea.Cmd {
	app := m.App
	webhook, service := app.Secrets[pcfg.SecretWebhook], agent.ServiceInstalled()
	oldHook, oldService := webhook, service
	f := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Background service").Description("Runs on this host: Prometheus metrics on "+orDash(app.Cfg.Metrics.Listen)+
			", Discord alerts when a server fails, keeps restarting or recovers, and automatic runtime updates."),
		huh.NewConfirm().Title("Run the background service?").Value(&service),
		huh.NewInput().Title("Discord webhook URL").Description("Server Settings → Integrations → Webhooks. Empty turns alerts off. A test message is sent.").Value(&webhook).
			Validate(func(s string) error {
				if s == "" || strings.HasPrefix(s, "https://") {
					return nil
				}
				return errors.New("a webhook URL starts with https://")
			}),
	).Title("Alerts and background service"))
	return m.ShowForm(f, func() tea.Cmd {
		webhook = strings.TrimSpace(webhook)
		return m.Run("Saving alerts and background service", func(ctx context.Context, say func(string)) error {
			if webhook != oldHook {
				if err := agent.SetWebhook(ctx, app, webhook); err != nil {
					return err
				}
				if webhook == "" {
					say("Discord alerts are off")
				} else {
					say("webhook saved; test message sent")
				}
			}
			switch {
			case service && (!oldService || webhook != oldHook):
				if err := agent.InstallService(m.Dir); err != nil {
					return err
				}
				say("background service evr-ptero-agent installed and running")
			case !service && oldService:
				if err := agent.RemoveService(); err != nil {
					return err
				}
				say("background service removed")
			}
			return nil
		}, nil)
	})
}

// Migration from older Echo eggs.

func (m *Model) loadLegacy() tea.Cmd {
	app := m.App
	m.flash, m.flashBad = "looking for servers on other Echo eggs …", false
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.Ctx, 30*time.Second)
		defer cancel()
		list, err := app.LegacyServers(ctx)
		return legacyMsg{list, err}
	}
}

func (m *Model) migrateForm(msg legacyMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		m.flash, m.flashBad = msg.err.Error(), true
		return nil
	case len(msg.list) == 0:
		m.flash, m.flashBad = "No servers on other Echo eggs found.", false
		return nil
	case !m.App.HasAccount():
		m.flash, m.flashBad = "Sign in first: migrated servers get this host's account.", true
		return m.openAccount()
	}
	m.flash = ""
	var opts []huh.Option[string]
	for _, s := range msg.list {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s, egg %q, port %d)", s.Server.Name, s.Server.Identifier, s.EggName, s.Port), s.Server.Identifier).Selected(true))
	}
	var picked []string
	confirm := false
	f := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().Title("Servers to move to the nevr-runtime egg").
			Description("Each keeps its allocation, owner and limits, gets this host's account and the shared game files, and restarts once.").
			Options(opts...).Value(&picked).Validate(func(v []string) error {
			if len(v) == 0 {
				return errors.New("pick at least one server")
			}
			return nil
		}),
		huh.NewConfirm().Title("Move them now?").Description("The old egg and game folders are left alone.").Value(&confirm),
	).Title("Move servers from an old Echo egg"))
	app := m.App
	return m.ShowForm(f, func() tea.Cmd {
		if !confirm {
			m.flash, m.flashBad = "cancelled", false
			return nil
		}
		want := map[string]bool{}
		for _, id := range picked {
			want[id] = true
		}
		var sel []core.LegacyServer
		for _, s := range msg.list {
			if want[s.Server.Identifier] {
				sel = append(sel, s)
			}
		}
		return m.Run(fmt.Sprintf("Moving %d server(s)", len(sel)), func(ctx context.Context, say func(string)) error {
			return app.MigrateServers(ctx, sel, say)
		}, nil)
	})
}

// Uninstall.

func (m *Model) uninstallForm() tea.Cmd {
	domain := m.App.Cfg.Panel.URL
	if u, err := url.Parse(domain); err == nil && u.Hostname() != "" {
		domain = u.Hostname()
	}
	var purge []string
	typed := ""
	f := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Uninstall the panel").Description(ui.Error.Render("This deletes the panel, its database, Wings and every server's files on this host. It cannot be undone.")+
			"\nThe shared game files in "+m.App.Cfg.DataDir+" are kept."),
		huh.NewMultiSelect[string]().Title("Also remove").Options(
			huh.NewOption("PHP, MariaDB, Redis, nginx and certbot", "packages"),
			huh.NewOption("Docker with all containers and images", "docker"),
			huh.NewOption("TLS certificates (Let's Encrypt allows 5 duplicates a week)", "certificates"),
		).Height(5).Value(&purge),
		huh.NewInput().Title("Type "+domain+" to confirm").Value(&typed).Validate(func(s string) error {
			if strings.TrimSpace(s) != domain {
				return errors.New("type the panel's domain exactly, or press esc")
			}
			return nil
		}),
	).Title("Uninstall"))
	return m.ShowForm(f, func() tea.Cmd {
		var o panelinstall.UninstallOptions
		for _, p := range purge {
			switch p {
			case "packages":
				o.Packages = true
			case "docker":
				o.Docker = true
			case "certificates":
				o.Certificates = true
			}
		}
		return m.Run("Uninstalling the panel", func(ctx context.Context, say func(string)) error {
			return panelinstall.Uninstall(ctx, o, &panelinstall.Runner{Log: say})
		}, func() tea.Cmd { return tea.Quit })
	})
}
