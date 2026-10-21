// evr-ptero runs Echo VR dedicated servers on Pterodactyl or Pelican.
package main

import (
	"os"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/cli"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/tui"
)

var version = "dev"

func main() {
	cli.TUI = tui.Run
	os.Exit(cli.Execute(version))
}
