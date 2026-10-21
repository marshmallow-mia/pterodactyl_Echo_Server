package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
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
