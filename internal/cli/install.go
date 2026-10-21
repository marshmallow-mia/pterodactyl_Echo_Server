package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/egg"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panelinstall"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// installFlags are shared by `panel install` and `init`.
type installFlags struct {
	o                 panelinstall.Options
	https             string
	dryRun, skipSetup bool
	bindIP, alias     string
	ports             int
	passwordStdin     bool
	noLink            bool
	account           struct{ discordID, httpKey, password string }
}

func addInstallFlags(cmd *cobra.Command, f *installFlags) {
	fl := cmd.Flags()
	fl.StringVar(&f.o.Product, "product", "pterodactyl", "pterodactyl or pelican")
	fl.StringVar(&f.o.Channel, "channel", "stable", "Pterodactyl: stable (1.x) or beta (2.0)")
	fl.StringVar(&f.o.FQDN, "fqdn", "", "domain name of the panel (required)")
	fl.StringVar(&f.o.Email, "email", "", "admin email for the panel login (required)")
	fl.StringVar(&f.o.LEEmail, "le-email", "", "email for the Let's Encrypt account (optional; expiry notices)")
	fl.StringVar(&f.o.AdminUser, "admin-user", "admin", "admin username")
	fl.StringVar(&f.o.AdminPassword, "admin-password", "", "admin password (generated when empty)")
	fl.StringVar(&f.o.Timezone, "timezone", "UTC", "panel timezone")
	fl.StringVar(&f.o.TLS, "tls", "letsencrypt", "letsencrypt, proxy (a reverse proxy terminates HTTPS, see --wings-fqdn), existing (--cert/--key) or none")
	fl.StringVar(&f.o.CertFile, "cert", "", "certificate file for --tls existing")
	fl.StringVar(&f.o.KeyFile, "key", "", "key file for --tls existing")
	fl.IntVar(&f.o.HTTPPort, "http-port", 80, "port nginx serves http on (behind NAT: the internal port that external 80 maps to)")
	fl.StringVar(&f.https, "https-ports", "443", "ports nginx serves https on, comma-separated (behind NAT e.g. 444,443)")
	fl.IntVar(&f.o.WingsPort, "wings-port", 8080, "Wings API port (browsers connect to it directly; forward it 1:1)")
	fl.IntVar(&f.o.SFTPPort, "sftp-port", 2022, "Wings SFTP port")
	fl.StringVar(&f.o.NodeName, "node-name", "", "node name (default: hostname)")
	fl.StringVar(&f.o.WingsFQDN, "wings-fqdn", "", "with --tls proxy: the proxy hostname for Wings (forwarding http to port 443 here)")
	fl.StringVar(&f.o.Database, "database", "mariadb", "Pelican: mariadb or sqlite")
	fl.StringVar(&f.o.PanelVersion, "panel-version", "", "override the panel version")
	fl.StringVar(&f.o.WingsVersion, "wings-version", "", "override the Wings version")
	fl.BoolVar(&f.dryRun, "dry-run", false, "print the commands instead of running them")
	fl.BoolVar(&f.skipSetup, "skip-setup", false, "only install the panel; do not prepare it for Echo VR")
	fl.StringVar(&f.bindIP, "bind-ip", "", "node IP for game port allocations (default: this host's main address)")
	fl.StringVar(&f.alias, "alias", "", "allocation alias shown in the panel (e.g. the public IP)")
	fl.IntVar(&f.ports, "ports", 10, "game ports to allocate from 6792")
	fl.StringVar(&f.account.discordID, "discord-id", "", "EchoVRCE host account Discord ID")
	fl.StringVar(&f.account.httpKey, "http-key", "", "EchoVRCE backend HTTP key (default: the public key)")
	fl.BoolVar(&f.passwordStdin, "password-stdin", false, "read the EchoVRCE password from stdin")
	fl.BoolVar(&f.noLink, "no-link", false, "without a password: do not link this host at echovrce.com/device now")
}

// linkAfterSetup links the host at echovrce.com/device when the setup left the
// servers without a sign-in, like the wizard does.
func linkAfterSetup(cmd *cobra.Command, g *globals, app *core.App, noLink bool) error {
	if app.HasAccount() {
		return nil
	}
	if noLink {
		g.say("No EchoVRCE sign-in yet. Link this host later with: evr-ptero account link")
		return nil
	}
	g.say("\nNo password given: linking this host at echovrce.com/device (skip with --no-link).")
	g.say("%s", core.ResetPasswordNote)
	_, err := app.UseDeviceLink(cmd.Context(), func(url, code string, expires time.Time) {
		g.say("Open %s and approve the code %s (valid until %s).", url, code, expires.Local().Format(time.TimeOnly))
	}, func(s string) { g.say("%s", s) })
	return err
}

func runInstall(cmd *cobra.Command, g *globals, f *installFlags) error {
	f.o.HTTPSPorts = panelinstall.Ports(f.https)
	if f.passwordStdin {
		p, err := readSecret("EchoVRCE server password: ")
		if err != nil {
			return err
		}
		f.account.password = p
	}
	r := &panelinstall.Runner{DryRun: f.dryRun, Log: func(s string) { g.say("%s", s) }}
	res, err := panelinstall.Install(cmd.Context(), f.o, r)
	if f.dryRun {
		fmt.Fprint(g.out, "#!/bin/bash\nset -euo pipefail\n"+r.Script.String())
		return err
	}
	if err != nil {
		return err
	}
	g.say("\nPanel: %s\nLogin: %s / %s\n(saved in /etc/evr-ptero/install.env)", res.URL, res.AdminEmail, res.AdminPassword)
	if f.skipSetup {
		return nil
	}
	bind := f.bindIP
	if bind == "" {
		bind = core.MainIP()
	}
	o := core.SetupOptions{URL: res.URL, APIURL: res.APIURL, AppKey: res.AppKey, ClientKey: res.ClientKey, NodeID: res.NodeID, BindIP: bind, Alias: f.alias, Ports: f.ports}
	o.Account.DiscordID, o.Account.HTTPKey, o.Account.Password = f.account.discordID, f.account.httpKey, f.account.password
	g.say("")
	app, err := core.Setup(cmd.Context(), g.dir, o, g.step)
	if err != nil {
		return err
	}
	return linkAfterSetup(cmd, g, app, f.noLink)
}

func panelInstallCmds(g *globals, panel *cobra.Command) {
	f := &installFlags{}
	install := &cobra.Command{
		Use:   "install",
		Short: "Install Pterodactyl (stable or beta) or Pelican with Wings on this host, then prepare it for Echo VR",
		Long: `Installs everything a panel needs on this Debian 12/13 host, unattended:
PHP, MariaDB, Redis, nginx, Let's Encrypt, the panel, Docker, Wings, a node
and API keys. It then runs "panel setup" for Echo VR. Every step checks first,
so it can be run again after a failure. --dry-run prints the commands instead.

Example behind a NAT that forwards 80→81, 443→444, 8080 and 2022:
  evr-ptero panel install --product pterodactyl --fqdn panel.example.com \
    --email admin@example.com --http-port 81 --https-ports 444,443`,
		RunE: func(cmd *cobra.Command, _ []string) error { return runInstall(cmd, g, f) },
	}
	addInstallFlags(install, f)
	panel.AddCommand(install)

	var u panelinstall.UninstallOptions
	var dry bool
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the panel, its database, Wings and ALL game servers on this host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !dry && !g.confirm("This deletes the panel, its database, Wings and every server's files on this host. Continue?") {
				return errors.New("cancelled (use --yes)")
			}
			r := &panelinstall.Runner{DryRun: dry, Log: func(s string) { g.say("%s", s) }}
			err := panelinstall.Uninstall(cmd.Context(), u, r)
			if dry {
				fmt.Fprint(g.out, r.Script.String())
			}
			return err
		},
	}
	uninstall.Flags().BoolVar(&u.Packages, "purge-packages", false, "also purge PHP, MariaDB, Redis, nginx and certbot")
	uninstall.Flags().BoolVar(&u.Docker, "purge-docker", false, "also purge Docker with all containers and images")
	uninstall.Flags().BoolVar(&u.Certificates, "purge-certificates", false, "also delete /etc/letsencrypt (Let's Encrypt allows 5 duplicate certificates a week)")
	uninstall.Flags().BoolVar(&dry, "dry-run", false, "print the commands instead of running them")
	panel.AddCommand(uninstall)
}

func initCmd(g *globals) *cobra.Command {
	f := &installFlags{}
	var mode, url, appKey, clientKey string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set everything up: install a panel or connect to one, then prepare it for Echo VR",
		Long: `Without flags in a terminal this opens the setup wizard.

  evr-ptero init --mode install --fqdn … --email …    install a panel here (see: panel install --help)
  evr-ptero init --mode connect --url … --app-key …   use an existing panel (see: panel setup --help)
  evr-ptero init --mode egg                           only write the egg files to import by hand`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().NFlag() == 0 && TUI != nil && isTerminal() {
				return TUI(cmd.Context(), g.dir)
			}
			switch mode {
			case "install":
				return runInstall(cmd, g, f)
			case "connect":
				o := core.SetupOptions{URL: url, AppKey: appKey, ClientKey: clientKey, BindIP: f.bindIP, Alias: f.alias, Ports: f.ports}
				o.Account.DiscordID, o.Account.HTTPKey = f.account.discordID, f.account.httpKey
				if f.passwordStdin {
					p, err := readSecret("EchoVRCE server password: ")
					if err != nil {
						return err
					}
					o.Account.Password = p
				}
				app, err := core.Setup(cmd.Context(), g.dir, o, g.step)
				if err != nil {
					return err
				}
				return linkAfterSetup(cmd, g, app, f.noLink)
			case "egg":
				for _, fm := range []string{"ptdl", "plcn"} {
					if err := writeEgg(fm, ""); err != nil {
						return err
					}
				}
				g.say("Wrote egg-echovr-nevr.json (Pterodactyl) and egg-echovr-nevr.yaml (Pelican).\nImport one in the panel, then see the README for the shared-files mounts.")
				return nil
			}
			return fmt.Errorf("--mode must be install, connect or egg")
		},
	}
	addInstallFlags(cmd, f)
	cmd.Flags().StringVar(&mode, "mode", "", "install, connect or egg")
	cmd.Flags().StringVar(&url, "url", "", "panel URL (connect)")
	cmd.Flags().StringVar(&appKey, "app-key", "", "application API key (connect)")
	cmd.Flags().StringVar(&clientKey, "client-key", "", "admin account API key (connect)")
	return cmd
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func writeEgg(format, image string) error {
	if image == "" {
		image = pcfg.DefaultImage
	}
	b, err := egg.Render(format, egg.Spec{Image: image, Version: core.Version})
	if err != nil {
		return err
	}
	name := "egg-echovr-nevr.json"
	if format == "plcn" {
		name = "egg-echovr-nevr.yaml"
	}
	return os.WriteFile(name, append(b, '\n'), 0o644)
}
