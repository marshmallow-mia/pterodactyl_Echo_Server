package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/egg"
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
			for _, fm := range []string{"ptdl", "plcn"} {
				b, err := egg.Render(fm, egg.Spec{Image: pcfg.DefaultImage, Version: core.Version})
				if err != nil {
					return err
				}
				name := map[string]string{"ptdl": "egg-echovr-nevr.json", "plcn": "egg-echovr-nevr.yaml"}[fm]
				if err := os.WriteFile(name, append(b, '\n'), 0o644); err != nil {
					return err
				}
				wd, _ := os.Getwd()
				say("wrote " + wd + "/" + name)
			}
			say("Import the .json in Pterodactyl (Admin → Nests → Import Egg) or the .yaml in Pelican (Eggs → Import).")
			return nil
		}, func() tea.Cmd { return tea.Quit })
	})
}

type account struct{ discordID, password, httpKey string }

func accountGroup(a *account) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().Title("Discord user ID").Description("The EchoVRCE account that hosts the servers.").Value(&a.discordID).Validate(digits),
		huh.NewInput().Title("Server password (optional)").Description("Leave empty to link this host at echovrce.com/device instead, right after setup.").EchoMode(huh.EchoModePassword).Value(&a.password),
	).Title("EchoVRCE host account")
}

// linkIfNoPassword runs the device link when no password was given.
func linkIfNoPassword(ctx context.Context, app *core.App, a *account, say func(string)) error {
	if a.password != "" {
		return nil
	}
	say("No password: linking this host at echovrce.com/device")
	s, err := app.LinkDevice(ctx, func(url, code string, expires time.Time) {
		say("Open " + url)
		say("and approve the code " + code + " (valid until " + expires.Local().Format(time.TimeOnly) + ")")
	})
	if err != nil {
		return err
	}
	say(fmt.Sprintf("Linked as %s; %s", s.Username, s.HostSummary()))
	return nil
}

func installForm(m *Model) tea.Cmd {
	o := panelinstall.Options{TLS: "letsencrypt"}
	product := "pterodactyl-stable"
	httpPort, httpsPorts, wingsPort, ports := "80", "443", "8080", "10"
	var alias string
	acct := &account{}
	f := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title("Panel").Options(
				huh.NewOption("Pterodactyl 1.15 (stable)", "pterodactyl-stable"),
				huh.NewOption("Pterodactyl 2.0 (beta)", "pterodactyl-beta"),
				huh.NewOption("Pelican 1.0 (release candidate)", "pelican"),
			).Value(&product),
			huh.NewInput().Title("Domain name").Description("Must point at this host's public IP.").Value(&o.FQDN).Validate(required("the domain")),
			huh.NewInput().Title("Admin email").Description("Your panel login.").Value(&o.Email).Validate(required("the email")),
			huh.NewSelect[string]().Title("HTTPS").Options(
				huh.NewOption("Let's Encrypt certificate (port 80 must be reachable)", "letsencrypt"),
				huh.NewOption("Behind a reverse proxy that handles HTTPS (Pangolin, Traefik, …)", "proxy"),
				huh.NewOption("I have a certificate", "existing"),
				huh.NewOption("No TLS (testing only)", "none"),
			).Value(&o.TLS),
			huh.NewInput().Title("Wings domain (reverse proxy only)").Description("A second proxy hostname that forwards plain http to port 443 of this host, auth off.").Value(&o.WingsFQDN),
			huh.NewInput().Title("Certificate and key files").Description("Only for 'I have a certificate': /path/fullchain.pem,/path/privkey.pem").Value(&o.CertFile),
		).Title("Install a panel"),
		huh.NewGroup(
			huh.NewNote().Title("Ports").Description("Behind a NAT that maps external ports to different internal ones, enter the internal ports.\n"+
				"The Wings port and game ports must be forwarded 1:1."),
			huh.NewInput().Title("HTTP port (internal)").Value(&httpPort).Validate(intRange(1, 65535)),
			huh.NewInput().Title("HTTPS ports (internal, comma-separated)").Description("Add 443 next to a NAT port so the host can reach itself by name.").Value(&httpsPorts),
			huh.NewInput().Title("Wings port").Value(&wingsPort).Validate(intRange(1, 65535)),
			huh.NewInput().Title("Game ports to allocate from 6792").Value(&ports).Validate(intRange(1, 200)),
			huh.NewInput().Title("Public IP (optional)").Description("Shown as allocation alias.").Value(&alias),
		),
		accountGroup(acct),
	)
	return m.ShowForm(f, func() tea.Cmd {
		o.Product, o.Channel = "pterodactyl", "stable"
		switch product {
		case "pterodactyl-beta":
			o.Channel = "beta"
		case "pelican":
			o.Product = "pelican"
		}
		if o.TLS == "existing" {
			o.CertFile, o.KeyFile, _ = strings.Cut(o.CertFile, ",")
		}
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
			say(fmt.Sprintf("Panel ready at %s, login %s / %s (also in /etc/evr-ptero/install.env)", res.URL, res.AdminEmail, res.AdminPassword))
			so := core.SetupOptions{URL: res.URL, APIURL: res.APIURL, AppKey: res.AppKey, ClientKey: res.ClientKey, NodeID: res.NodeID, Alias: alias, Ports: n}
			so.Account.DiscordID, so.Account.Password, so.Account.HTTPKey = acct.discordID, acct.password, acct.httpKey
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
	f := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Panel URL").Description("Empty uses the panel on this host.").Value(&url),
			huh.NewInput().Title("Application API key").Description(desc).EchoMode(huh.EchoModePassword).Value(&appKey),
			huh.NewInput().Title("Account API key of an admin (optional)").Description("Needed to start servers and show their console.").EchoMode(huh.EchoModePassword).Value(&clientKey),
			huh.NewInput().Title("Node IP for game ports").Description("The node address allocations use (empty: existing allocations).").Value(&bindIP),
			huh.NewInput().Title("Public IP (optional)").Value(&alias),
			huh.NewInput().Title("Game ports to allocate from 6792").Value(&ports).Validate(intRange(1, 200)),
		).Title("Use an existing panel"),
		accountGroup(acct),
	)
	return m.ShowForm(f, func() tea.Cmd {
		n, _ := strconv.Atoi(ports)
		return m.Run("Preparing the panel", func(ctx context.Context, say func(string)) error {
			so := core.SetupOptions{URL: url, AppKey: appKey, ClientKey: clientKey, BindIP: bindIP, Alias: alias, Ports: n}
			so.Account.DiscordID, so.Account.Password, so.Account.HTTPKey = acct.discordID, acct.password, acct.httpKey
			app, err := core.Setup(ctx, m.Dir, so, func(step, detail string) { say(step + ": " + detail) })
			if err != nil {
				return err
			}
			m.App = app
			return linkIfNoPassword(ctx, app, acct, say)
		}, func() tea.Cmd { return m.LoadServers() })
	})
}
