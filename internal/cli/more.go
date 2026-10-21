package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/updates"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/agent"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// legacyCredentials reads the account from an old game directory: config.yaml
// (nevr 4.x) or the serverdb/login URLs in config.json (EchoRelay era).
func legacyCredentials(gameDir string) (discordID, password, httpKey string) {
	if b, err := os.ReadFile(filepath.Join(gameDir, "_local", "config.yaml")); err == nil {
		if c, err := config.Parse(b); err == nil {
			discordID, password, httpKey = c.Identity.DiscordID, c.Auth.Password, c.Auth.HTTPKey
		}
	}
	if discordID != "" && password != "" {
		return
	}
	var cj map[string]any
	if b, err := os.ReadFile(filepath.Join(gameDir, "_local", "config.json")); err == nil && json.Unmarshal(b, &cj) == nil {
		for _, k := range []string{"serverdb_host", "loginservice_host"} {
			s, _ := cj[k].(string)
			if u, err := url.Parse(s); err == nil {
				q := u.Query()
				if discordID == "" {
					discordID = q.Get("discordid")
				}
				if password == "" {
					password = q.Get("password")
				}
			}
		}
	}
	return
}

func runtimeCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "runtime", Short: "nevr-runtime versions in the node's shared files"}
	cmd.AddCommand(&cobra.Command{
		Use: "ls", Short: "List installed versions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			st := a.Store()
			list, err := st.List()
			if err != nil {
				return err
			}
			return g.print(list, func(w io.Writer) {
				var rows [][]string
				for _, r := range list {
					state := "installed"
					switch r.Tag {
					case st.Current():
						state = "current"
					case st.Previous():
						state = "previous"
					}
					rows = append(rows, []string{r.Tag, state, r.Bad})
				}
				g.table("VERSION\tSTATE\tNOTE", rows)
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "use TAG", Short: "Switch the node's current runtime (servers pick it up after their match)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			return a.Store().Use(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "remove TAG", Short: "Delete an installed version that is not in use", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			return a.Store().Remove(args[0])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "rollback", Short: "Switch back to the previous runtime",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			tag, err := a.Store().Rollback()
			if err == nil {
				g.say("rolled back to %s", tag)
			}
			return err
		},
	})
	return cmd
}

func migrateCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "migrate", Short: "Move servers from an old Echo egg to the nevr-runtime egg"}
	detect := &cobra.Command{
		Use: "detect", Short: "List servers that use another Echo VR egg",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			list, err := a.LegacyServers(cmd.Context())
			if err != nil {
				return err
			}
			return g.print(list, func(w io.Writer) {
				if len(list) == 0 {
					fmt.Fprintln(w, "No servers on other Echo eggs found.")
					return
				}
				for _, s := range list {
					fmt.Fprintf(w, "  %-24s %s  egg %q  port %d\n", s.Server.Name, s.Server.Identifier, s.EggName, s.Port)
				}
			})
		},
	}
	cmd.AddCommand(detect)
	run := &cobra.Command{
		Use: "run [SERVER...]", Short: "Switch servers to the new egg, image and shared files (same allocation), then restart them",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			list, err := a.LegacyServers(cmd.Context())
			if err != nil {
				return err
			}
			if len(list) == 0 {
				return errors.New("no servers on other Echo eggs found")
			}
			want := map[string]bool{}
			for _, s := range args {
				want[s] = true
			}
			var sel []core.LegacyServer
			for _, s := range list {
				if len(want) == 0 || want[s.Server.Name] || want[s.Server.Identifier] {
					sel = append(sel, s)
				}
			}
			for _, s := range sel {
				g.say("  %s (%s, port %d)", s.Server.Name, s.Server.Identifier, s.Port)
			}
			if !g.confirm(fmt.Sprintf("Migrate these %d server(s)? They restart once.", len(sel))) {
				return errors.New("cancelled (use --yes)")
			}
			return a.MigrateServers(cmd.Context(), sel, func(s string) { g.say("%s", s) })
		},
	}
	cmd.AddCommand(run)
	return cmd
}

func agentCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Background service: metrics, Discord alerts, runtime updates"}
	cmd.AddCommand(&cobra.Command{
		Use: "run", Short: "Run in the foreground (systemd runs this)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			l := log.New(os.Stderr, "", 0)
			return (&agent.Agent{Dir: g.dir, Log: func(s string) { l.Println(s) }}).Run(cmd.Context())
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "install", Short: "Install and start the agent as a systemd service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := agent.InstallService(g.dir); err != nil {
				return err
			}
			g.say("evr-ptero-agent installed and started")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "uninstall", Short: "Stop and remove the agent service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := agent.RemoveService(); err != nil {
				return err
			}
			g.say("evr-ptero-agent removed")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "webhook [URL|off]", Short: "Set (or test) the Discord webhook for alerts", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				url := args[0]
				if url == "off" {
					url = ""
				}
				if err := agent.SetWebhook(cmd.Context(), a, url); err != nil {
					return err
				}
				if url != "" {
					g.say("saved; test message sent")
					return nil
				}
			}
			if a.Secrets[pcfg.SecretWebhook] == "" {
				g.say("Discord alerts are off")
				return nil
			}
			if err := agent.TestWebhook(cmd.Context(), a, a.Secrets[pcfg.SecretWebhook]); err != nil {
				return err
			}
			g.say("test message sent")
			return nil
		},
	})
	return cmd
}

// component finds an updatable part by name.
func component(comps []updates.Component, name string) (updates.Component, error) {
	alias := map[string]string{"evr-ptero": "self", "nevr-runtime": "runtime", "nevr": "runtime"}
	if a, ok := alias[name]; ok {
		name = a
	}
	var names []string
	for _, c := range comps {
		if c.Name() == name {
			return c, nil
		}
		names = append(names, c.Name())
	}
	return nil, fmt.Errorf("unknown part %q on this host (one of: %s)", name, strings.Join(names, ", "))
}

func printStatus(g *globals, list []updates.Status, comps []updates.Component, set updates.Settings) error {
	return g.print(list, func(w io.Writer) {
		title := map[string]string{}
		for _, c := range comps {
			title[c.Name()] = c.Title()
		}
		var rows [][]string
		for _, s := range list {
			avail := "up to date"
			switch {
			case s.Error != "":
				avail = "check failed: " + s.Error
			case s.Available:
				avail = s.Latest
			}
			rows = append(rows, []string{s.Component, title[s.Component], s.Installed, avail, string(set.Mode(s.Component))})
		}
		g.table("PART\tNAME\tINSTALLED\tAVAILABLE\tMODE", rows)
	})
}

func updateCmd(g *globals) *cobra.Command {
	check := func(cmd *cobra.Command, args []string) error {
		a, err := g.app()
		if err != nil {
			return err
		}
		comps, set := agent.Components(a, nil), a.Cfg.Updates.Settings()
		if len(args) == 1 {
			c, err := component(comps, args[0])
			if err != nil {
				return err
			}
			return printStatus(g, []updates.Status{updates.CheckOne(cmd.Context(), a.Dir, c)}, comps, set)
		}
		return printStatus(g, updates.CheckAll(cmd.Context(), a.Dir, comps, set), comps, set)
	}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Updates of evr-ptero itself, the game files, nevr-runtime, the panel and Wings",
		Long: `Without a subcommand: check every part. Parts: self (evr-ptero), game (the
node's shared game files; repairs them from the mirror), runtime (nevr-runtime,
tried on one idle server first), panel and wings (when they run on this host;
the panel is backed up first and restored if the update fails).
"update settings" chooses per part: off, notify (check and alert) or auto
(install; needs the agent).`,
		Args: cobra.NoArgs,
		RunE: check,
	}
	cmd.AddCommand(&cobra.Command{Use: "check [PART]", Short: "Check for updates (all parts, or one)", Args: cobra.MaximumNArgs(1), RunE: check})
	cmd.AddCommand(&cobra.Command{
		Use: "ls PART", Short: "List the versions a part can be updated to", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			c, err := component(agent.Components(a, nil), args[0])
			if err != nil {
				return err
			}
			vs, err := c.Versions(cmd.Context())
			if err != nil {
				return err
			}
			return g.print(vs, func(w io.Writer) {
				if len(vs) == 0 {
					fmt.Fprintln(w, c.Title()+" has no versions to choose from; apply repairs it from the mirror.")
				}
				for _, v := range vs {
					fmt.Fprintln(w, v)
				}
			})
		},
	})
	var version string
	var dry bool
	apply := &cobra.Command{
		Use: "apply [PART]", Short: "Install an update now (default part: runtime)", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			name := "runtime"
			if len(args) == 1 {
				name = args[0]
			}
			say := func(s string) { g.say("%s", s) }
			c, err := component(agent.Components(a, say), name)
			if err != nil {
				return err
			}
			if dry {
				d, ok := c.(agent.DryRunner)
				if !ok {
					return fmt.Errorf("%s has no dry run", c.Title())
				}
				script, err := d.DryRun(cmd.Context(), version)
				fmt.Fprint(g.out, "#!/bin/bash\nset -euo pipefail\n"+script)
				return err
			}
			if p, ok := c.(*agent.Panel); ok && version != "" && p.SwitchesChannel(cmd.Context(), version) &&
				!g.confirm("Panel "+version+" is in another channel. This migrates the database one way; a backup is taken first. Continue?") {
				return errors.New("cancelled (use --yes)")
			}
			if err := c.Apply(cmd.Context(), version, say); err != nil {
				return err
			}
			updates.CheckOne(cmd.Context(), a.Dir, c)
			return nil
		},
	}
	apply.Flags().StringVar(&version, "version", "", "version to install (default: the newest; see update ls)")
	apply.Flags().BoolVar(&dry, "dry-run", false, "panel and wings: print the commands instead of running them")
	cmd.AddCommand(apply)
	var self, game, rt, pnl, wings, window, channel string
	var hours int
	settings := &cobra.Command{
		Use: "settings", Short: "Show or change how each part is updated",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			u := &a.Cfg.Updates
			fl := cmd.Flags()
			for _, f := range []struct {
				flag string
				dst  *string
				val  string
			}{{"self", &u.Self, self}, {"game", &u.Game, game}, {"runtime", &u.Runtime, rt}, {"panel", &u.Panel, pnl}, {"wings", &u.Wings, wings},
				{"window", &u.Window, window}, {"panel-channel", &u.PanelChannel, channel}} {
				if fl.Changed(f.flag) {
					*f.dst = f.val
				}
			}
			if fl.Changed("check-hours") {
				u.CheckHours = hours
			}
			if fl.NFlag() > 0 {
				if err := a.Save(); err != nil {
					return err
				}
			}
			return g.print(u, func(w io.Writer) {
				fmt.Fprintf(w, "self:          %s\ngame:          %s\nruntime:       %s\npanel:         %s\nwings:         %s\npanel channel: %s\nwindow:        %s\ncheck every:   %s\n",
					u.Self, u.Game, u.Runtime, u.Panel, u.Wings, dash(u.PanelChannel), dash(u.Window), a.Cfg.Updates.Settings().Interval())
				if !agent.ServiceInstalled() {
					fmt.Fprintln(w, "\nThe agent is not installed: nothing is checked or installed in the background (evr-ptero agent install).")
				}
			})
		},
	}
	sf := settings.Flags()
	for _, f := range []struct {
		dst        *string
		name, what string
	}{{&self, "self", "evr-ptero itself"}, {&game, "game", "the shared game files"}, {&rt, "runtime", "nevr-runtime"}, {&pnl, "panel", "the panel"}, {&wings, "wings", "Wings"}} {
		sf.StringVar(f.dst, f.name, "", f.what+": off, notify or auto")
	}
	sf.StringVar(&window, "window", "", `automatic installs only in this local time window, e.g. 04:00-06:00 ("" = any time)`)
	sf.StringVar(&channel, "panel-channel", "", `panel releases to follow: stable or beta (Pterodactyl; "" = the installed one)`)
	sf.IntVar(&hours, "check-hours", 0, "hours between checks")
	cmd.AddCommand(settings)
	return cmd
}

func postUpdateCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use: "post-update", Hidden: true, Short: "Run after a self-update by the new binary: move the egg and servers to its image",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := core.Setup(cmd.Context(), g.dir, core.SetupOptions{}, g.step)
			return err
		},
	}
}
