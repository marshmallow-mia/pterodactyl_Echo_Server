package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/match"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/status"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
)

// switchSpec describes an on/off switch of a server's match stats.
type switchSpec struct {
	name, short, long, label string
	get                      func(a *core.App, s core.Server) bool
	running                  func(st *status.Status) string
	set                      func(a *core.App, ctx context.Context, s core.Server, on bool) error
}

func serverAPICmd(g *globals) *cobra.Command {
	return switchCmd(g, switchSpec{
		name:  "api",
		short: "Switch match stats (the game's session API) on or off",
		long: "Match stats come from the game's HTTP API on 127.0.0.1 inside each container (never reachable from outside);\n" +
			"the supervisor writes them to match.json and match-history.json in the server's volume, where the panel's\n" +
			"Echo VR match plugin and the dashboard read them. On by default. An idle server restarts its game to apply\n" +
			"a change; one in a match applies it after the match. Panel users with console access can switch it too.",
		label:   "match stats",
		get:     (*core.App).SessionAPI,
		running: func(st *status.Status) string { return st.SessionAPI },
		set:     (*core.App).SetSessionAPI,
	})
}

func serverTelemetryCmd(g *globals) *cobra.Command {
	return switchCmd(g, switchSpec{
		name:  "telemetry",
		short: "Switch nevr-runtime's telemetry stream to the supervisor on or off",
		long: "With telemetry on, nevr-runtime streams the match to the supervisor on 127.0.0.1 inside the container\n" +
			"(never to anyone else). The supervisor reports the connection; the play-by-play comes from the session API,\n" +
			"because nevr-runtime 6.16.4 sends most player stat events wrong (EVR_TELEMETRY_PLAYS=1 uses them anyway).\n" +
			"On by default. An idle server restarts its game to apply a change; one in a match applies it after the match.",
		label:   "telemetry",
		get:     (*core.App).Telemetry,
		running: func(st *status.Status) string { return st.Telemetry },
		set:     (*core.App).SetTelemetry,
	})
}

func switchCmd(g *globals, sp switchSpec) *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   sp.name + " on|off|status [SERVER...]",
		Short: sp.short,
		Long:  sp.long,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			action := strings.ToLower(args[0])
			if action != "on" && action != "off" && action != "status" {
				return fmt.Errorf("unknown action %q: use on, off or status", args[0])
			}
			servers, err := pick(cmd, a, args[1:], all || (action == "status" && len(args) == 1))
			if err != nil {
				return err
			}
			if action == "status" {
				type row struct {
					Name, Setting, Running string
				}
				var rows []row
				for _, s := range servers {
					r := row{Name: s.Name(), Setting: onOff(sp.get(a, s))}
					if s.Status != nil && s.Power == "running" {
						r.Running = sp.running(s.Status)
					}
					rows = append(rows, r)
				}
				return g.print(rows, func(w io.Writer) {
					var t [][]string
					for _, r := range rows {
						note := ""
						if r.Running != "" && r.Running != r.Setting {
							note = "the game picks it up at its next start"
						}
						t = append(t, []string{r.Name, r.Setting, dash(r.Running), note})
					}
					g.table("NAME\tSETTING\tRUNNING GAME\tNOTE", t)
				})
			}
			var errs []error
			for _, s := range servers {
				if err := sp.set(a, cmd.Context(), s, action == "on"); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
					continue
				}
				g.say("%s: %s %s (an idle server restarts its game now, one in a match after it)", s.Name(), sp.label, action)
			}
			return errors.Join(errs...)
		},
	}
	c.Flags().BoolVar(&all, "all", false, "all Echo VR servers")
	return c
}

func serverMatchCmd(g *globals) *cobra.Command {
	var history int
	c := &cobra.Command{
		Use: "match SERVER", Short: "Show a server's current or last match (match stats)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			s, err := a.Find(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			m, ok := a.Match(s)
			hist, err := a.MatchHistory(s)
			if err != nil {
				return err
			}
			if history >= 0 && len(hist) > history {
				hist = hist[:history]
			}
			out := struct {
				Match   *match.Match    `json:"match"`
				History []match.Summary `json:"history"`
			}{History: hist}
			if ok {
				out.Match = &m
			}
			return g.print(out, func(w io.Writer) {
				if !ok {
					fmt.Fprintln(w, "No match yet (or the server's image is older than match stats).")
				} else {
					match.Fprint(w, m)
				}
				match.FprintHistory(w, hist)
			})
		},
	}
	c.Flags().IntVar(&history, "history", 10, "finished matches to list (-1 for all)")
	return c
}

// pick resolves server references, or every Echo VR server with all.
func pick(cmd *cobra.Command, a *core.App, refs []string, all bool) ([]core.Server, error) {
	if all {
		return a.Servers(cmd.Context())
	}
	if len(refs) == 0 {
		return nil, errors.New("name a server or use --all")
	}
	var out []core.Server
	for _, ref := range refs {
		s, err := a.Find(cmd.Context(), ref)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
