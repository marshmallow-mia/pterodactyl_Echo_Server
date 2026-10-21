package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/notify"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/update"
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
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if exe, err = filepath.EvalSymlinks(exe); err != nil {
				return err
			}
			unit := fmt.Sprintf("[Unit]\nDescription=evr-ptero agent (Echo VR server metrics, alerts, updates)\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nExecStart=%s --config-dir %s agent run\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\n", exe, g.dir)
			if err := os.WriteFile("/etc/systemd/system/evr-ptero-agent.service", []byte(unit), 0o644); err != nil {
				return err
			}
			for _, args := range [][]string{{"daemon-reload"}, {"enable", "evr-ptero-agent.service"}, {"restart", "evr-ptero-agent.service"}} {
				if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
					return fmt.Errorf("systemctl %v: %v: %s", args, err, out)
				}
			}
			g.say("evr-ptero-agent installed and started")
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
				if args[0] == "off" {
					delete(a.Secrets, pcfg.SecretWebhook)
				} else {
					a.Secrets[pcfg.SecretWebhook] = args[0]
				}
				if err := a.Secrets.Save(a.Dir); err != nil {
					return err
				}
			}
			if a.Secrets[pcfg.SecretWebhook] == "" {
				g.say("Discord alerts are off")
				return nil
			}
			host, _ := os.Hostname()
			if err := (&notify.Notifier{URL: a.Secrets[pcfg.SecretWebhook]}).Send(cmd.Context(), notify.Event{Kind: "test", Level: notify.Info, Host: host,
				Title: "evr-ptero alerts are set up", Text: "This channel will get alerts about the Echo VR servers in " + a.Cfg.Panel.URL + "."}); err != nil {
				return err
			}
			g.say("test message sent")
			return nil
		},
	})
	return cmd
}

func updateCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "update", Short: "Check for and roll out nevr-runtime updates on the node"}
	cmd.AddCommand(&cobra.Command{
		Use: "check", Short: "Is a newer runtime available?",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			cur, target, available, err := agent.Updater(a, nil).Check(cmd.Context(), update.Policy{Channel: a.Cfg.Runtime.Channel, Pinned: a.Cfg.Runtime.Pinned})
			if err != nil {
				return err
			}
			if available {
				g.say("nevr-runtime %s is available (current %s); roll it out with: evr-ptero update apply", target.Tag, cur)
			} else {
				g.say("up to date (%s)", cur)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "apply", Short: "Canary on an idle server, then switch the node's current runtime",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			res, err := agent.Updater(a, func(s string) { g.say("%s", s) }).Run(cmd.Context(), update.Policy{Channel: a.Cfg.Runtime.Channel, Pinned: a.Cfg.Runtime.Pinned})
			if err != nil {
				return err
			}
			return g.print(res, func(w io.Writer) { fmt.Fprintf(w, "%s: %s → %s %s\n", res.Action, res.From, res.To, res.Detail) })
		},
	})
	return cmd
}
