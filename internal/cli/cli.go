// Package cli is evr-ptero's command line; the TUI calls the same core functions.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/egg"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// TUI is set by main.
var TUI func(ctx context.Context, dir string) error

type globals struct {
	dir  string
	json bool
	yes  bool
	out  io.Writer
}

// Execute runs the CLI.
func Execute(version string) int {
	core.Version = version
	pcfg.SetVersion(version)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g := &globals{out: os.Stdout}
	root := &cobra.Command{
		Use:   "evr-ptero",
		Short: "Run Echo VR dedicated servers (nevr-runtime) on Pterodactyl or Pelican",
		Long: `evr-ptero sets up Pterodactyl or Pelican for Echo VR servers and manages them:
the egg, the node's shared game files, servers, runtime updates, alerts and metrics.

Run it without arguments for the interactive TUI. Nothing here is required:
you can import egg/egg-echovr-nevr.json yourself and manage everything in the panel.`,
		SilenceUsage: true, SilenceErrors: true, Version: version,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if TUI == nil || !term.IsTerminal(int(os.Stdout.Fd())) {
				return cmd.Help()
			}
			return TUI(cmd.Context(), g.dir)
		},
	}
	root.PersistentFlags().StringVar(&g.dir, "config-dir", pcfg.Dir(), "configuration directory")
	root.PersistentFlags().BoolVar(&g.json, "json", false, "print JSON")
	root.PersistentFlags().BoolVarP(&g.yes, "yes", "y", false, "do not ask for confirmation")
	root.AddCommand(initCmd(g), panelCmd(g), eggCmd(g), filesCmd(g), serverCmd(g), accountCmd(g), runtimeCmd(g), migrateCmd(g), agentCmd(g), updateCmd(g))
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func (g *globals) app() (*core.App, error) { return core.Open(g.dir) }

func (g *globals) say(f string, a ...any) {
	if !g.json {
		fmt.Fprintf(g.out, f+"\n", a...)
	}
}

func (g *globals) step(step, detail string) { g.say("[%s] %s", step, detail) }

func (g *globals) print(v any, text func(io.Writer)) error {
	if g.json || text == nil {
		enc := json.NewEncoder(g.out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	text(g.out)
	return nil
}

func (g *globals) table(header string, rows [][]string) {
	tw := tabwriter.NewWriter(g.out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, header)
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

func (g *globals) confirm(q string) bool {
	if g.yes {
		return true
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Fprintf(g.out, "%s [y/N] ", q)
	var a string
	fmt.Scanln(&a)
	return strings.EqualFold(a, "y") || strings.EqualFold(a, "yes")
}

func readSecret(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		return strings.TrimSpace(string(b)), err
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(string(b)), err
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func panelCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "panel", Short: "Connect to and prepare the panel"}
	var o core.SetupOptions
	var passwordStdin bool
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Prepare the panel: API keys, node, shared files, egg, mounts, allocations",
		Long: `Connects to the panel and prepares it for Echo VR servers. Safe to run again.

On the panel host with Pterodactyl 1.x, API keys are created automatically and
the egg and mounts are installed directly. With a remote panel pass --app-key
(and --client-key for power and console); on Pterodactyl 1.x import the egg and
create the mounts yourself when asked.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if passwordStdin {
				p, err := readSecret("EchoVRCE server password: ")
				if err != nil {
					return err
				}
				o.Account.Password = p
			}
			_, err := core.Setup(cmd.Context(), g.dir, o, g.step)
			return err
		},
	}
	f := setup.Flags()
	f.StringVar(&o.URL, "url", "", "panel URL (default: APP_URL of a panel on this host)")
	f.StringVar(&o.AppKey, "app-key", "", "application API key")
	f.StringVar(&o.ClientKey, "client-key", "", "account API key of an admin (power and console)")
	f.IntVar(&o.NodeID, "node", 0, "node ID (default: this host's node)")
	f.StringVar(&o.BindIP, "bind-ip", "", "node IP to create allocations on")
	f.StringVar(&o.Alias, "alias", "", "allocation alias shown in the panel (e.g. the public IP)")
	f.IntVar(&o.Ports, "ports", 0, "make sure this many ports from 6792 are allocated")
	f.StringVar(&o.GameSource, "game-source", "", "mount (shared files, default) or download (copy per server)")
	f.StringVar(&o.DataDir, "data-dir", "", "shared files on the node (default /srv/echovr)")
	f.StringVar(&o.Image, "image", "", "server image (default "+pcfg.DefaultImage+")")
	f.IntVar(&o.OwnerID, "owner", 0, "panel user ID that owns new servers (default: first admin)")
	f.BoolVar(&o.SkipFiles, "skip-files", false, "do not install the shared game files now")
	f.StringVar(&o.Account.DiscordID, "discord-id", "", "EchoVRCE host account Discord ID")
	f.StringVar(&o.Account.HTTPKey, "http-key", "", "EchoVRCE backend HTTP key (default: the public key)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the EchoVRCE password from stdin")
	cmd.AddCommand(setup)
	panelInstallCmds(g, cmd)
	cmd.AddCommand(&cobra.Command{
		Use: "info", Short: "Show the panel connection",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			c := a.Cfg.Panel
			return g.print(c, func(w io.Writer) {
				fmt.Fprintf(w, "Panel:  %s (%s, local %t)\nNode:   #%d\nEgg:    #%d (nest %q)\nMounts: %v\nOwner:  user #%d\nPorts:  on %s (alias %s)\nShared files: %s (%s)\n",
					c.URL, c.Kind, c.Local, c.NodeID, c.EggID, c.Nest, c.MountIDs, c.OwnerID, c.BindIP, c.Alias, a.Cfg.DataDir, a.Cfg.GameSource)
			})
		},
	})
	return cmd
}

func eggCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "egg", Short: "Export or update the Echo VR egg"}
	var format, image, out string
	export := &cobra.Command{
		Use: "export", Short: "Write the egg for importing by hand (ptdl = Pterodactyl, plcn = Pelican)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if image == "" {
				image = pcfg.DefaultImage
				if a, err := g.app(); err == nil {
					image = a.Cfg.Image
				}
			}
			b, err := egg.Render(format, egg.Spec{Image: image, Version: core.Version})
			if err != nil {
				return err
			}
			if out == "-" || out == "" {
				_, err = g.out.Write(append(b, '\n'))
				return err
			}
			return os.WriteFile(out, append(b, '\n'), 0o644)
		},
	}
	export.Flags().StringVar(&format, "format", "ptdl", "ptdl (Pterodactyl JSON) or plcn (Pelican YAML)")
	export.Flags().StringVar(&image, "image", "", "server image")
	export.Flags().StringVarP(&out, "output", "o", "-", "file to write")
	cmd.AddCommand(export)
	return cmd
}

func filesCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "files", Short: "Shared game files and runtimes on the node"}
	var dir, runtime, mirror string
	install := &cobra.Command{
		Use: "install", Short: "Install the game and nevr-runtime into a directory (the egg's install script uses this too)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				dir = pcfg.DefaultDataDir
				if a, err := g.app(); err == nil {
					dir = a.Cfg.DataDir
				}
			}
			last := ""
			return core.InstallFiles(cmd.Context(), dir, runtime, mirror, func(step, detail string) {
				line := "[" + step + "] " + detail
				if strings.HasPrefix(step, "Game: Downloading") {
					if line != last {
						fmt.Fprintf(g.out, "\r\033[K%s", line)
						last = line
					}
					return
				}
				if last != "" {
					fmt.Fprintln(g.out)
					last = ""
				}
				fmt.Fprintln(g.out, line)
			})
		},
	}
	install.Flags().StringVar(&dir, "dir", "", "target directory (default: data_dir, /srv/echovr)")
	install.Flags().StringVar(&runtime, "runtime", "current", `nevr-runtime tag or "current"`)
	install.Flags().StringVar(&mirror, "mirror", "", "rsync mirror for the game files")
	cmd.AddCommand(install)
	return cmd
}

func serverCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "server", Aliases: []string{"servers", "s"}, Short: "Echo VR servers in the panel"}
	cmd.AddCommand(&cobra.Command{
		Use: "ls", Aliases: []string{"list"}, Short: "List Echo VR servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			list, err := a.Servers(cmd.Context())
			if err != nil {
				return err
			}
			type row struct {
				Name, Identifier, State, Runtime, Registered string
				Port, Matches, Restarts                      int
			}
			var rows []row
			for _, s := range list {
				r := row{Name: s.Name(), Identifier: s.Panel.Identifier, State: s.State(), Port: s.Port}
				if st := s.Status; st != nil {
					r.Runtime, r.Matches, r.Restarts = st.RuntimeVersion, st.SessionsTotal, st.Restarts
					if st.PublicIP != "" {
						r.Registered = fmt.Sprintf("%s:%d", st.PublicIP, st.Port)
					}
				}
				rows = append(rows, r)
			}
			return g.print(rows, func(w io.Writer) {
				if len(rows) == 0 {
					fmt.Fprintln(w, "No Echo VR servers yet. Create some with: evr-ptero server create --count 2")
					return
				}
				var t [][]string
				for _, r := range rows {
					t = append(t, []string{r.Name, r.Identifier, strconv.Itoa(r.Port), r.State, dash(r.Runtime), dash(r.Registered), strconv.Itoa(r.Matches), strconv.Itoa(r.Restarts)})
				}
				g.table("NAME\tID\tPORT\tSTATE\tRUNTIME\tREGISTERED AS\tMATCHES\tRESTARTS", t)
			})
		},
	})
	var count int
	var regions, guilds string
	create := &cobra.Command{
		Use: "create", Short: "Create servers on free ports of the node and start them",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			var set instance.Settings
			if cmd.Flags().Changed("regions") {
				set.Regions = splitList(regions)
			}
			if cmd.Flags().Changed("guilds") {
				set.Guilds = splitList(guilds)
			}
			_, err = a.CreateServers(cmd.Context(), count, set, func(s string) { g.say("%s", s) })
			return err
		},
	}
	create.Flags().IntVarP(&count, "count", "n", 1, "how many servers")
	create.Flags().StringVar(&regions, "regions", "", "comma-separated regions")
	create.Flags().StringVar(&guilds, "guilds", "", "comma-separated guild IDs")
	cmd.AddCommand(create)
	for _, sig := range []string{"start", "stop", "restart", "kill"} {
		sig := sig
		cmd.AddCommand(&cobra.Command{
			Use: sig + " SERVER...", Short: strings.ToUpper(sig[:1]) + sig[1:] + " servers (name, ID or UUID)", Args: cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := g.app()
				if err != nil {
					return err
				}
				var errs []error
				for _, ref := range args {
					s, err := a.Find(cmd.Context(), ref)
					if err == nil {
						err = a.Power(cmd.Context(), s, sig)
					}
					if err != nil {
						errs = append(errs, fmt.Errorf("%s: %w", ref, err))
					} else {
						g.say("%s: %s sent", s.Name(), sig)
					}
				}
				return errors.Join(errs...)
			},
		})
	}
	cmd.AddCommand(&cobra.Command{
		Use: "reinstall SERVER", Short: "Run the egg's install step again (e.g. after switching GAME_SOURCE)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			s, err := a.Find(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := a.Panel().Reinstall(cmd.Context(), s.Panel.ID); err != nil {
				return err
			}
			g.say("%s: reinstalling", s.Name())
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "rm SERVER", Short: "Delete a server from the panel", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			s, err := a.Find(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !g.confirm("Delete " + s.Name() + " from the panel?") {
				return errors.New("cancelled (use --yes)")
			}
			return a.Delete(cmd.Context(), s)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "logs SERVER", Short: "Follow a server's console", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			s, err := a.Find(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.Console(cmd.Context(), s, func(l string) { fmt.Fprintln(g.out, l) })
		},
	})
	var eRegions, eGuilds, eRuntime string
	var eTick, eTTL int
	var noRestart bool
	edit := &cobra.Command{
		Use: "edit SERVER", Short: "Change a server's settings (restarts it if running)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			s, err := a.Find(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fl := cmd.Flags()
			return a.UpdateServer(cmd.Context(), s, func(set *instance.Settings) {
				if fl.Changed("regions") {
					set.Regions = splitList(eRegions)
				}
				if fl.Changed("guilds") {
					set.Guilds = splitList(eGuilds)
				}
				if fl.Changed("runtime") {
					set.Runtime = eRuntime
				}
				if fl.Changed("tick-rate") {
					set.TickRate = eTick
				}
				if fl.Changed("empty-ttl") {
					set.EmptyTTL = eTTL
				}
			}, !noRestart)
		},
	}
	edit.Flags().StringVar(&eRegions, "regions", "", "comma-separated regions")
	edit.Flags().StringVar(&eGuilds, "guilds", "", "comma-separated guild IDs")
	edit.Flags().StringVar(&eRuntime, "runtime", "", `nevr-runtime tag or "current"`)
	edit.Flags().IntVar(&eTick, "tick-rate", 0, "tick rate (10-1000)")
	edit.Flags().IntVar(&eTTL, "empty-ttl", 0, "empty server TTL in seconds")
	edit.Flags().BoolVar(&noRestart, "no-restart", false, "apply at the next restart")
	cmd.AddCommand(edit)
	return cmd
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func accountCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "account", Short: "EchoVRCE host account"}
	var discordID, httpKey, gameDir string
	var passwordStdin, applyAll, noPassword bool
	set := &cobra.Command{
		Use: "set", Short: "Store the account (and with --apply update every server)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			if gameDir != "" {
				id, pw, key := legacyCredentials(gameDir)
				if id == "" {
					return fmt.Errorf("no credentials found in %s/_local", gameDir)
				}
				a.Secrets[config.EnvDiscordID], a.Secrets[config.EnvPassword] = id, pw
				if key != "" {
					a.Secrets[config.EnvHTTPKey] = key
				}
			}
			if discordID != "" {
				a.Secrets[config.EnvDiscordID] = discordID
			}
			if httpKey != "" {
				a.Secrets[config.EnvHTTPKey] = httpKey
			}
			// Ask for what is still missing when someone is at the terminal.
			if a.Secrets[config.EnvDiscordID] == "" && term.IsTerminal(int(os.Stdin.Fd())) {
				fmt.Fprint(os.Stderr, "Discord user ID of the EchoVRCE host account: ")
				var id string
				fmt.Scanln(&id)
				a.Secrets[config.EnvDiscordID] = strings.TrimSpace(id)
			}
			if noPassword {
				delete(a.Secrets, config.EnvPassword)
				if err := a.Secrets.Save(a.Dir); err != nil {
					return err
				}
				if !a.DeviceLinked() {
					g.say("saved without a password; now link this host: evr-ptero account link")
					return nil
				}
			} else if passwordStdin || a.Secrets[config.EnvPassword] == "" {
				p, err := readSecret("EchoVRCE server password: ")
				if err != nil {
					return err
				}
				a.Secrets[config.EnvPassword] = p
			}
			s, err := a.Login(cmd.Context())
			if err != nil {
				return err
			}
			if err := a.Secrets.Save(a.Dir); err != nil {
				return err
			}
			g.say("saved; signed in as %s, %s", s.Username, s.HostSummary())
			if !applyAll {
				return nil
			}
			list, err := a.Servers(cmd.Context())
			if err != nil {
				return err
			}
			for _, sv := range list {
				if err := a.UpdateServer(cmd.Context(), sv, func(*instance.Settings) {}, true); err != nil {
					return fmt.Errorf("%s: %w", sv.Name(), err)
				}
				g.say("%s: updated", sv.Name())
			}
			return nil
		},
	}
	set.Flags().StringVar(&discordID, "discord-id", "", "Discord user ID")
	set.Flags().StringVar(&httpKey, "http-key", "", "backend HTTP key (default: the public key nevr-runtime embeds)")
	set.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin")
	set.Flags().BoolVar(&noPassword, "no-password", false, "no password: sign in with a device link instead (evr-ptero account link)")
	set.Flags().StringVar(&gameDir, "from-game-dir", "", "take the account from an old game directory's _local/config.yaml or config.json")
	set.Flags().BoolVar(&applyAll, "apply", false, "also update every Echo VR server with it")
	cmd.AddCommand(set)
	cmd.AddCommand(&cobra.Command{
		Use:   "link",
		Short: "Link this host at echovrce.com/device (sign in without a password)",
		Long: `Shows a short code to approve at echovrce.com/device and stores the resulting
refresh token in /etc/evr-ptero/device.json, and takes the Discord ID from the
backend account. New servers get the link in their _local/.credentials.json.
No password or key has to be entered.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			g.say("%s\n", core.ResetPasswordNote)
			s, err := a.LinkDevice(cmd.Context(), func(url, code string, expires time.Time) {
				g.say("Open %s and approve the code %s (valid until %s).", url, code, expires.Local().Format(time.TimeOnly))
			})
			if err != nil {
				return err
			}
			g.say("Linked as %s (Discord ID %s); %s.", s.Username, a.Secrets[config.EnvDiscordID], s.HostSummary())
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "test", Short: "Sign in and list hostable guilds",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			s, err := a.Login(cmd.Context())
			if err != nil {
				return err
			}
			return g.print(map[string]any{"username": s.Username, "host_guilds": s.HostGuilds()}, func(w io.Writer) {
				fmt.Fprintf(w, "Signed in as %s; %s\n", s.Username, s.HostSummary())
				for _, gid := range s.HostGuilds() {
					fmt.Fprintln(w, "  "+gid)
				}
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "servers", Short: "Servers the backend has registered for the account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			s, err := a.Login(cmd.Context())
			if err != nil {
				return err
			}
			list, err := a.Auth().MyServers(cmd.Context(), s)
			if err != nil {
				return err
			}
			return g.print(list, func(w io.Writer) {
				var rows [][]string
				for _, sv := range list {
					rows = append(rows, []string{sv.CreatedAt.Local().Format(time.DateTime), sv.Region, sv.LobbyType, strconv.Itoa(sv.Players)})
				}
				g.table("REGISTERED\tREGION\tLOBBY\tPLAYERS", rows)
			})
		},
	})
	return cmd
}
