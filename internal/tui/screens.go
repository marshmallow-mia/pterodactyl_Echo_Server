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

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/game"
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
	legacyScanMsg []core.LegacyServer
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
		if core.DeviceLinkEnabled() {
			return m, m.linkDevice()
		}
	case "p":
		return m, m.passwordForm()
	case "o":
		return m, m.oldGameForm()
	}
	return m, nil
}

// oldGameForm takes the account from an old game folder's _local config.
func (m *Model) oldGameForm() tea.Cmd {
	app := m.App
	dir := core.LegacyGameDir
	var acc core.LegacyAccount
	f := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Account from an old installation").Description("Reads the Discord ID and password the old game folder keeps in its\nconfig.json or config.yaml. The v1 guide used "+core.LegacyGameDir+"."),
		huh.NewInput().Title("Old game folder").Value(&dir).Validate(func(s string) error {
			id, pw, key := core.LegacyCredentials(strings.TrimSpace(s))
			switch {
			case id == "":
				return errors.New("no account found in " + filepath.Join(s, "_local"))
			case pw == "":
				return errors.New("only a Discord ID there, no password; press esc and use p")
			}
			acc = core.LegacyAccount{DiscordID: id, Password: pw, HTTPKey: key, Dir: strings.TrimSpace(s)}
			return nil
		}),
	).Title("EchoVRCE account"))
	return m.ShowForm(f, func() tea.Cmd {
		return m.Run("Signing in", func(ctx context.Context, say func(string)) error {
			return app.UseLegacyAccount(ctx, acc, say)
		}, func() tea.Cmd { return m.openAccount() })
	})
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
		huh.NewNote().Title("Password sign-in").Description(passwordNote),
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
	linkOn := core.DeviceLinkEnabled()
	var b strings.Builder
	b.WriteString(m.header() + "  " + ui.Bold.Render("account") + "\n\n")
	a := m.account
	switch {
	case a == nil:
		b.WriteString(m.spin.View() + " signing in …\n")
	case errors.Is(a.err, core.ErrNoAccount):
		b.WriteString(ui.Warning.Render("Not signed in yet: the servers cannot register with EchoVRCE.") + "\n\n")
		b.WriteString("Press " + ui.KeyStyle.Render("p") + " to sign in with your Discord ID and server password,\n")
		b.WriteString("or " + ui.KeyStyle.Render("o") + " to take them from an old installation's game folder.\n")
		b.WriteString(ui.Subtle.Render(m.wrap(passwordNote, 0)) + "\n")
		if linkOn {
			b.WriteString("\nOr press " + ui.KeyStyle.Render("l") + " to link this host at echovrce.com/device (nevr-runtime 5.x only).\n")
			b.WriteString(ui.Subtle.Render(m.wrap(core.ResetPasswordNote, 0)) + "\n")
		}
	case a.err != nil:
		b.WriteString(ui.Error.Render(m.wrap(a.err.Error(), 0)) + "\n\n")
		if linkOn {
			b.WriteString(ui.Subtle.Render("Press p to use a password, or l to link this host again.") + "\n")
		} else {
			b.WriteString(ui.Subtle.Render("Press p to sign in with your Discord ID and server password.") + "\n")
		}
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
	keys := []ui.Key{ui.K("p", "password sign-in"), ui.K("o", "from old install")}
	if linkOn {
		keys = append(keys, ui.K("l", "link this host"))
	}
	return b.String() + m.footer(append(keys, ui.K("r", "refresh"), ui.K("esc", "back"))...)
}

// The "more" menu.

func (m *Model) menu() tea.Cmd {
	choice := "updates"
	opts := []huh.Option[string]{
		huh.NewOption("Updates (evr-ptero, game files, nevr-runtime, panel, Wings)", "updates"),
		huh.NewOption("Runtime versions", "runtime"),
		huh.NewOption("Discord alerts and the background service", "alerts"),
		huh.NewOption("EchoVRCE account", "account"),
		huh.NewOption("Move servers from an old Echo egg", "migrate"),
		huh.NewOption("Panel connection", "panel"),
		huh.NewOption("Panel admin login and password", "login"),
		huh.NewOption("Re-run the panel setup (egg, mounts, allocations, game files)", "setup"),
		huh.NewOption("Write the egg files", "egg"),
	}
	if m.App.Cfg.Panel.Local {
		opts = append(opts, huh.NewOption("Uninstall the panel from this host", "uninstall"))
	}
	f := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("What do you want to do?").Options(opts...).Value(&choice)).Title("More"))
	return m.ShowForm(f, func() tea.Cmd {
		switch choice {
		case "updates":
			return m.openUpdates()
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
		case "login":
			return m.panelLoginForm()
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
	if email, _, _ := panelinstall.AdminLogin(); email != "" {
		row("Panel login", email+" (More → Panel admin login and password)")
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
	channel, pinned, auto := app.Cfg.Runtime.Channel, app.Cfg.Runtime.Pinned, app.Cfg.Updates.Runtime == "auto"
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
			huh.NewInput().Title("Pinned version").Description("e.g. v6.6.0").Value(&pinned).Validate(required("the version")),
		).WithHideFunc(func() bool { return channel != "pinned" }),
	)
	return m.ShowForm(f, func() tea.Cmd {
		app.Cfg.Runtime.Channel, app.Cfg.Updates.Runtime = channel, "notify"
		if auto {
			app.Cfg.Updates.Runtime = "auto"
		}
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
	case m.App.Cfg.Updates.Runtime == "auto" && agent.ServiceInstalled():
		policy += "; the background service rolls them out automatically."
	case m.App.Cfg.Updates.Runtime == "auto":
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

// scanLegacy looks for old-egg servers in the background, for the dashboard notice.
func (m *Model) scanLegacy() tea.Cmd {
	app := m.App
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.Ctx, 30*time.Second)
		defer cancel()
		list, err := app.LegacyServers(ctx)
		if err != nil {
			return legacyScanMsg(nil)
		}
		return legacyScanMsg(app.UnhiddenLegacy(list))
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
	}
	m.flash = ""
	app := m.App
	var opts []huh.Option[string]
	for _, s := range msg.list {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s, egg %q, port %d)", s.Server.Name, s.Server.Identifier, s.EggName, s.Port), s.Server.Identifier).Selected(true))
	}
	var picked []string
	action := "move"
	first := huh.NewGroup(
		huh.NewMultiSelect[string]().Title("Servers to move to the nevr-runtime egg").
			Description("Each gets this host's account, the shared game files and nevr-runtime, on the same port.").
			Options(opts...).Height(min(len(opts), 8)+4).Value(&picked).Validate(func(v []string) error {
			if len(v) == 0 && action == "move" {
				return errors.New("pick at least one server")
			}
			return nil
		}),
		huh.NewSelect[string]().Title("Move them now?").Options(
			huh.NewOption("Move now", "move"),
			huh.NewOption("Not now", "later"),
			huh.NewOption("Not now, and stop pointing at these servers on the dashboard", "hide"),
		).Value(&action),
	).Title("Move servers from an old Echo egg")
	notMoving := func() bool { return action != "move" }

	// How: recreate or keep, explained; the old logs; what to delete.
	mode := core.MigrateRecreate
	var modeOpts []huh.Option[string]
	explain := map[string]string{}
	for _, md := range core.MigrateModes {
		modeOpts = append(modeOpts, huh.NewOption(md.Title, md.Key))
		explain[md.Key] = md.Explain
	}
	files := app.FindLegacyFiles()
	logs := core.LogsMove
	var remove []string
	what := map[string]string{}
	var ropts []huh.Option[string]
	for _, r := range app.Removable(msg.list, files) {
		ropts = append(ropts, huh.NewOption(r.What, r.Key))
		what[r.Key] = r.What
	}
	how := []huh.Field{
		huh.NewSelect[string]().Title("How should they be moved?").Options(modeOpts...).Value(&mode).
			DescriptionFunc(func() string { return explain[mode] }, &mode),
	}
	if files.GameDir != "" {
		how = append(how, huh.NewSelect[string]().Title("Old logs ("+game.Size(files.LogsSize)+" in "+filepath.Join(files.GameDir, "logs")+")").Options(
			huh.NewOption("Move them to "+filepath.Join(app.Cfg.DataDir, game.V1LogsDir), core.LogsMove),
			huh.NewOption("Keep them in the old game folder", core.LogsKeep),
			huh.NewOption("Delete them", core.LogsDelete),
		).Value(&logs))
	}
	if len(ropts) > 0 {
		how = append(how, huh.NewMultiSelect[string]().Title("Also delete, once the servers are moved").
			Description("Nothing is deleted unless you pick it here.").Options(ropts...).Height(len(ropts)+5).Value(&remove).
			Validate(func(v []string) error {
				return core.MigrateOptions{Mode: mode, Logs: logs, Remove: v}.Validate()
			}))
	}
	groups := []*huh.Group{
		first,
		huh.NewGroup(how...).Title("How").WithHideFunc(notMoving),
	}

	// Without an account the move signs in first, preferably with the one the old servers used.
	acct, discordID, password := "", "", ""
	var old core.LegacyAccount
	needAccount := !app.HasAccount()
	if needAccount {
		var found bool
		old, found = app.FindLegacyAccount(msg.list)
		var aopts []huh.Option[string]
		aopts, acct = migrateAccountOptions(old, found)
		groups = append(groups,
			huh.NewGroup(huh.NewSelect[string]().Title("EchoVRCE account for the moved servers").Options(aopts...).Value(&acct)).
				WithHideFunc(notMoving),
			huh.NewGroup(
				huh.NewInput().Title("Discord user ID").Description("Discord → Settings → Advanced → Developer mode, then copy your ID.").Value(&discordID).Validate(digits),
				huh.NewInput().Title("Server password").Description("The password you set for hosting with the EchoVRCE bot.").EchoMode(huh.EchoModePassword).Value(&password).Validate(required("the password")),
			).Title("Password sign-in").WithHideFunc(func() bool { return notMoving() || acct != "password" }))
	}
	return m.ShowForm(huh.NewForm(groups...), func() tea.Cmd {
		switch action {
		case "later":
			m.flash, m.flashBad = "Not moved. m → Move servers from an old Echo egg when you are ready.", false
			return nil
		case "hide":
			if err := app.HideLegacy(msg.list); err != nil {
				m.flash, m.flashBad = err.Error(), true
				return nil
			}
			m.legacy = nil
			m.flash, m.flashBad = "Not moved. They stay under m → Move servers from an old Echo egg.", false
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
		start := func(sure bool) tea.Cmd {
			cmd := m.Run(fmt.Sprintf("Moving %d server(s)", len(sel)), func(ctx context.Context, say func(string)) error {
				if needAccount {
					var err error
					switch acct {
					case "legacy":
						err = app.UseLegacyAccount(ctx, old, say)
					case "password":
						_, err = app.UsePassword(ctx, discordID, password, say)
					case "link":
						say(core.ResetPasswordNote)
						say("")
						_, err = app.UseDeviceLink(ctx, func(url, code string, expires time.Time) {
							say("Open " + url)
							say("and approve the code " + code + " (valid until " + expires.Local().Format(time.TimeOnly) + ")")
							say("waiting …")
						}, say)
					}
					if err != nil {
						return err
					}
					say("")
				}
				opts := core.MigrateOptions{Mode: mode, Logs: logs}
				if sure {
					opts.Remove = remove
				}
				return app.MigrateServers(ctx, sel, files, opts, say)
			}, func() tea.Cmd {
				// Until both answer, the dashboard says it is loading rather than "press a".
				m.legacy, m.serversLoaded, m.legacyChecked = nil, false, false
				return tea.Batch(m.LoadServers(), m.scanLegacy())
			})
			m.busyCanCancel = needAccount && acct == "link"
			return cmd
		}
		if len(remove) == 0 {
			return start(false)
		}
		var items []string
		for _, k := range remove {
			items = append(items, what[k])
		}
		return m.confirmDelete(items, start)
	})
}

// confirmDelete asks once more before an old installation's parts are
// deleted; then runs with whether they may be.
func (m *Model) confirmDelete(items []string, then func(sure bool) tea.Cmd) tea.Cmd {
	sure := false
	f := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Delete these for sure, once the servers are moved?").
			Description("• " + strings.Join(items, "\n• ") + "\nThis cannot be undone.").
			Affirmative("Yes, delete them").Negative("No, keep them").Value(&sure),
	))
	return m.ShowForm(f, func() tea.Cmd { return then(sure) })
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

// Panel admin login.

func (m *Model) panelLoginForm() tea.Cmd {
	app := m.App
	email, user, stored := panelinstall.AdminLogin()
	action := "set"
	desc := "The panel was not installed by evr-ptero on this host, so its password is not known here.\nYou can still set a new one for a panel user."
	if email != "" {
		desc = fmt.Sprintf("Panel:    %s\nEmail:    %s\nUsername: %s", app.Cfg.Panel.URL, email, user)
	}
	opts := []huh.Option[string]{
		huh.NewOption("Set a new password", "set"),
		huh.NewOption("Generate a new password", "generate"),
	}
	if stored != "" {
		opts = append([]huh.Option[string]{huh.NewOption("Show the current password", "show")}, opts...)
		action = "show"
	}
	target, password, again := email, "", ""
	f := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Panel admin login").Description(desc),
			huh.NewSelect[string]().Title("What do you want to do?").Options(opts...).Value(&action),
		).Title("Panel login"),
		huh.NewGroup(
			huh.NewInput().Title("Panel user (email)").Description("Empty: the admin.").Value(&target),
			huh.NewInput().Title("New password").Description(fmt.Sprintf("At least %d characters.", core.MinPanelPassword)).EchoMode(huh.EchoModePassword).Value(&password).
				Validate(func(s string) error {
					if len(s) < core.MinPanelPassword {
						return fmt.Errorf("at least %d characters", core.MinPanelPassword)
					}
					return nil
				}),
			huh.NewInput().Title("New password again").EchoMode(huh.EchoModePassword).Value(&again).Validate(func(s string) error {
				if s != password {
					return errors.New("the passwords do not match")
				}
				return nil
			}),
		).WithHideFunc(func() bool { return action != "set" }),
		huh.NewGroup(
			huh.NewInput().Title("Panel user (email)").Description("Empty: the admin.").Value(&target),
		).WithHideFunc(func() bool { return action != "generate" }),
	)
	return m.ShowForm(f, func() tea.Cmd {
		if action == "show" {
			m.screen, m.infoTitle = scrInfo, "panel login"
			m.infoText = fmt.Sprintf("  Panel     %s\n  Email     %s\n  Username  %s\n  Password  %s\n\n", app.Cfg.Panel.URL, email, user, stored) +
				ui.Subtle.Render("  Kept in "+filepath.Join(m.Dir, "install.env")+" (root only). Change it here with More → Panel admin login and password.") + "\n"
			return nil
		}
		if action == "generate" {
			password = panelinstall.RandomPassword()
		}
		return m.Run("Changing the panel password", func(ctx context.Context, say func(string)) error {
			u, err := app.SetPanelPassword(ctx, target, password)
			if err != nil {
				return err
			}
			if action == "generate" {
				say(fmt.Sprintf("New password for %s (%s): %s", u.Email, u.Username, password))
			} else {
				say(fmt.Sprintf("Password of %s (%s) changed.", u.Email, u.Username))
			}
			return nil
		}, nil)
	})
}

// passwordNote says where the server password comes from.
const passwordNote = "nevr-runtime 6.x signs in with the host account's Discord ID and server password\n(set it for hosting with the EchoVRCE bot); it refuses a server that has only a device link."

// migrateAccountOptions are the sign-ins a move without an account offers, and
// the default: the old servers' account if there is one, else the password.
func migrateAccountOptions(old core.LegacyAccount, found bool) ([]huh.Option[string], string) {
	var opts []huh.Option[string]
	def := "password"
	if found {
		def = "legacy"
		opts = append(opts, huh.NewOption(fmt.Sprintf("The old servers' account (Discord ID %s, from %s)", old.DiscordID, old.Dir), "legacy"))
	}
	opts = append(opts, huh.NewOption("Discord ID and server password", "password"))
	if core.DeviceLinkEnabled() {
		opts = append(opts, huh.NewOption("Link this host at echovrce.com/device (nevr-runtime 5.x only)", "link"))
	}
	return opts, def
}
