package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func panelPluginCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "plugin", Short: "The panel's Echo VR match plugin (live match stats per server)"}
	cmd.AddCommand(&cobra.Command{
		Use: "status", Short: "Show the installed and the bundled plugin version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			installed, status, bundled := a.PluginStatus()
			out := map[string]string{"installed": installed, "status": status, "bundled": bundled}
			return g.print(out, func(w io.Writer) {
				fmt.Fprintf(w, "Installed: %s (%s)\nBundled:   %s\n", dash(installed), dash(status), bundled)
				if a.Cfg.Panel.NoPlugin {
					fmt.Fprintln(w, "Removed on purpose: setup leaves it out until \"evr-ptero panel plugin install\".")
				}
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "install", Short: "Install or update the plugin in this host's Pelican panel",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			line, err := a.InstallPlugin(cmd.Context())
			if err != nil {
				return err
			}
			g.say("%s", line)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "remove", Short: "Uninstall the plugin (setup and updates leave it out from then on)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := g.app()
			if err != nil {
				return err
			}
			if err := a.RemovePlugin(cmd.Context()); err != nil {
				return err
			}
			g.say("echovr-match removed")
			return nil
		},
	})
	return cmd
}
