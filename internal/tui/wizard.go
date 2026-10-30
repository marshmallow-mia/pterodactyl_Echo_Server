package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panelinstall"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

func init() { Wizard = startWizard }

func required(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(what + " is required")
		}
		return nil
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func digits(s string) error {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return errors.New("digits only")
	}
	return nil
}

func startWizard(m *Model) tea.Cmd {
	mode := "connect"
	local, hasLocal := panel.DetectLocal()
	if !hasLocal {
		mode = "install"
	}
	intro := "Run Echo VR dedicated servers with nevr-runtime on Pterodactyl or Pelican.\n\n" +
		"Nothing here is required: you can also import the egg yourself and manage everything in the panel."
	if hasLocal {
		intro += fmt.Sprintf("\n\nFound %s in %s on this host.", local.Kind, local.Dir)
	}
	f := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Welcome to evr-ptero").Description(intro),
		huh.NewSelect[string]().Title("What do you want to do?").Options(
			huh.NewOption("Install a panel with Wings on this host", "install"),
			huh.NewOption("Use an existing panel", "connect"),
			huh.NewOption("Only write the egg files (import them yourself)", "egg"),
		).Value(&mode),
	))
	return m.ShowForm(f, func() tea.Cmd {
		switch mode {
		case "install":
			return installForm(m)
		case "connect":
			return connectForm(m, hasLocal)
		}
		return m.Run("Writing the egg files", func(ctx context.Context, say func(string)) error {
			return writeEggFiles(pcfg.DefaultImage, say)
		}, func() tea.Cmd { return tea.Quit })
	})
}

type account struct{ signIn, discordID, password, httpKey string }

// fields returns the Discord ID and password to store (none for the device link).
func (a *account) fields() (discordID, password string) {
	if a.signIn == "password" {
		return a.discordID, a.password
	}
	return "", ""
}

// accountGroups ask how the servers sign in. The password is the default
// (nevr-runtime 6.x needs it); the device link is offered only with
// --device-link, and the Discord ID and password only for the password sign-in.
func accountGroups(a *account) []*huh.Group {
	a.signIn = "password"
	linkOn := core.DeviceLinkEnabled()
	return []*huh.Group{
		huh.NewGroup(
			huh.NewSelect[string]().Title("How should the servers sign in?").Options(
				huh.NewOption("Discord ID and server password (needed for nevr-runtime 6.x)", "password"),
				huh.NewOption("Device link at echovrce.com/device (nevr-runtime 5.x only)", "device"),
			).Value(&a.signIn),
			huh.NewNote().Title("Device link").Description(core.ResetPasswordNote+"\nThe code to approve is shown after the setup; the Discord ID comes from your account."),
		).Title("EchoVRCE account").WithHideFunc(func() bool { return !linkOn }),
		huh.NewGroup(
			huh.NewNote().Description(passwordNote),
			huh.NewInput().Title("Discord user ID").Description("Discord → Settings → Advanced → Developer mode, then copy your ID.").Value(&a.discordID).Validate(digits),
			huh.NewInput().Title("Server password").Description("The password you set for hosting with the EchoVRCE bot.").EchoMode(huh.EchoModePassword).Value(&a.password).Validate(required("the password")),
		).Title("EchoVRCE account").WithHideFunc(func() bool { return a.signIn != "password" }),
	}
}

// linkIfNoPassword runs the device link when no password was given and the
// device link is on (core.DeviceLinkEnabled). If it fails, the busy screen
// offers to try again.
func linkIfNoPassword(ctx context.Context, app *core.App, a *account, say func(string)) error {
	if a.signIn == "password" || !core.DeviceLinkEnabled() {
		return nil
	}
	say("")
	say("Linking this host at echovrce.com/device")
	say(core.ResetPasswordNote)
	_, err := app.UseDeviceLink(ctx, func(url, code string, expires time.Time) {
		say("Open " + url)
		say("and approve the code " + code + " (valid until " + expires.Local().Format(time.TimeOnly) + ")")
		say("waiting …")
	}, say)
	if err != nil {
		return linkFailed{fmt.Errorf("device link: %w", err)}
	}
	return nil
}

func installForm(m *Model) tea.Cmd {
	o := panelinstall.Options{TLS: "letsencrypt"}
	product := "pterodactyl-stable"
	httpPort, httpsPorts, wingsPort, ports := "80", "443", "8080", "10"
	var alias, adminPassword, adminAgain string
	acct := &account{}
	f := huh.NewForm(append([]*huh.Group{
		huh.NewGroup(
			huh.NewSelect[string]().Title("Panel").Options(
				huh.NewOption("Pterodactyl 1.15 (stable)", "pterodactyl-stable"),
				huh.NewOption("Pterodactyl 2.0 (beta)", "pterodactyl-beta"),
				huh.NewOption("Pelican 1.0 (release candidate)", "pelican"),
			).Value(&product),
			huh.NewInput().Title("Domain name").Description("Must point at this host's public IP.").Value(&o.FQDN).Validate(required("the domain")),
			huh.NewInput().Title("Admin email").Description("Your panel login.").Value(&o.Email).Validate(required("the email")),
			huh.NewInput().Title("Admin password").Description(fmt.Sprintf("At least %d characters. Leave empty to have one generated (shown at the end and kept in %s).", core.MinPanelPassword, panelinstall.StateFile())).
				EchoMode(huh.EchoModePassword).Value(&adminPassword).Validate(func(s string) error {
				if s != "" && len(s) < core.MinPanelPassword {
					return fmt.Errorf("at least %d characters", core.MinPanelPassword)
				}
				return nil
			}),
			huh.NewInput().Title("Admin password again").EchoMode(huh.EchoModePassword).Value(&adminAgain).Validate(func(s string) error {
				if s != adminPassword {
					return errors.New("the passwords do not match")
				}
				return nil
			}),
			huh.NewSelect[string]().Title("HTTPS").Options(
				huh.NewOption("Let's Encrypt certificate (port 80 must be reachable)", "letsencrypt"),
				huh.NewOption("Behind a reverse proxy that handles HTTPS (Pangolin, Traefik, …)", "proxy"),
				huh.NewOption("I have a certificate", "existing"),
				huh.NewOption("No TLS (testing only)", "none"),
			).Value(&o.TLS),
		).Title("Install a panel"),
		huh.NewGroup(
			huh.NewNote().Title("Reverse proxy").DescriptionFunc(func() string {
				return fmt.Sprintf("Set up two resources in your proxy, both forwarding plain http to this host:\n"+
					"  %s → http://<this host>:<HTTP port below> (the panel)\n"+
					"  Wings domain → http://<this host>:443 (Wings, no proxy login in front of it)", orDefault(o.FQDN, "panel domain"))
			}, &o.FQDN),
			huh.NewInput().Title("Wings domain").Description("A second hostname on your proxy for Wings (console and file access).").Value(&o.WingsFQDN).Validate(required("the Wings domain")),
		).WithHideFunc(func() bool { return o.TLS != "proxy" }),
		huh.NewGroup(
			huh.NewInput().Title("Certificate file").Description("Full chain, e.g. /etc/ssl/panel/fullchain.pem").Value(&o.CertFile).Validate(required("the certificate")),
			huh.NewInput().Title("Key file").Value(&o.KeyFile).Validate(required("the key")),
		).WithHideFunc(func() bool { return o.TLS != "existing" }),
		huh.NewGroup(
			huh.NewNote().Title("Ports").Description("Behind a NAT that maps external ports to different internal ones, enter the internal ports.\n"+
				"Game ports must be forwarded 1:1."),
			huh.NewInput().Title("HTTP port (internal)").Value(&httpPort).Validate(intRange(1, 65535)),
		),
		huh.NewGroup(
			huh.NewInput().Title("HTTPS ports (internal, comma-separated)").Description("Add 443 next to a NAT port so the host can reach itself by name.").Value(&httpsPorts),
			huh.NewInput().Title("Wings port").Description("Must be forwarded 1:1.").Value(&wingsPort).Validate(intRange(1, 65535)),
		).WithHideFunc(func() bool { return o.TLS == "proxy" }),
		huh.NewGroup(
			huh.NewInput().Title("Game ports to allocate from 6792").Value(&ports).Validate(intRange(1, 200)),
			huh.NewInput().Title("Public IP (optional)").Description("Shown as allocation alias.").Value(&alias),
		),
	}, accountGroups(acct)...)...)
	return m.ShowForm(f, func() tea.Cmd {
		o.Product, o.Channel = "pterodactyl", "stable"
		switch product {
		case "pterodactyl-beta":
			o.Channel = "beta"
		case "pelican":
			o.Product = "pelican"
		}
		o.AdminPassword = adminPassword
		o.HTTPPort, _ = strconv.Atoi(httpPort)
		o.WingsPort, _ = strconv.Atoi(wingsPort)
		o.HTTPSPorts = panelinstall.Ports(httpsPorts)
		n, _ := strconv.Atoi(ports)
		return m.Run("Installing "+product, func(ctx context.Context, say func(string)) error {
			r := &panelinstall.Runner{Log: say}
			res, err := panelinstall.Install(ctx, o, r)
			if err != nil {
				return err
			}
			if adminPassword == "" {
				say(fmt.Sprintf("Panel ready at %s, login %s with the generated password %s", res.URL, res.AdminEmail, res.AdminPassword))
			} else {
				say(fmt.Sprintf("Panel ready at %s, login %s with your password", res.URL, res.AdminEmail))
			}
			say("(m → Panel admin login shows or changes it later)")
			so := core.SetupOptions{URL: res.URL, APIURL: res.APIURL, AppKey: res.AppKey, ClientKey: res.ClientKey, NodeID: res.NodeID, Alias: alias, Ports: n}
			so.Account.DiscordID, so.Account.Password = acct.fields()
			so.Account.HTTPKey = acct.httpKey
			app, err := core.Setup(ctx, m.Dir, so, func(step, detail string) { say(step + ": " + detail) })
			if err != nil {
				return err
			}
			m.App = app
			return linkIfNoPassword(ctx, app, acct, say)
		}, func() tea.Cmd { return m.LoadServers() })
	})
}

func connectForm(m *Model, local bool) tea.Cmd {
	var url, appKey, clientKey, bindIP, alias string
	ports := "10"
	acct := &account{}
	desc := "Admin → Application API (Pterodactyl) or Admin → API Keys (Pelican)."
	if local {
		desc = "The panel runs here: leave the keys empty and they are created for you."
	}
	f := huh.NewForm(append([]*huh.Group{
		huh.NewGroup(
			huh.NewInput().Title("Panel URL").Description("Empty uses the panel on this host.").Value(&url),
			huh.NewInput().Title("Application API key").Description(desc).EchoMode(huh.EchoModePassword).Value(&appKey),
			huh.NewInput().Title("Account API key of an admin (optional)").Description("Needed to start servers and show their console.").EchoMode(huh.EchoModePassword).Value(&clientKey),
			huh.NewInput().Title("Node IP for game ports").Description("The node address allocations use (empty: existing allocations).").Value(&bindIP),
			huh.NewInput().Title("Public IP (optional)").Value(&alias),
			huh.NewInput().Title("Game ports to allocate from 6792").Value(&ports).Validate(intRange(1, 200)),
		).Title("Use an existing panel"),
	}, accountGroups(acct)...)...)
	return m.ShowForm(f, func() tea.Cmd {
		n, _ := strconv.Atoi(ports)
		return m.Run("Preparing the panel", func(ctx context.Context, say func(string)) error {
			so := core.SetupOptions{URL: url, AppKey: appKey, ClientKey: clientKey, BindIP: bindIP, Alias: alias, Ports: n}
			so.Account.DiscordID, so.Account.Password = acct.fields()
			so.Account.HTTPKey = acct.httpKey
			app, err := core.Setup(ctx, m.Dir, so, func(step, detail string) { say(step + ": " + detail) })
			if err != nil {
				return err
			}
			m.App = app
			return linkIfNoPassword(ctx, app, acct, say)
		}, func() tea.Cmd { return m.LoadServers() })
	})
}
