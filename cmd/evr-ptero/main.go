// evr-ptero runs Echo VR dedicated servers on Pterodactyl or Pelican.
package main

import (
	"os"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/cli"
)

var version = "dev"

func main() { os.Exit(cli.Execute(version)) }
