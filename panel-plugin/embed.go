// Package panelplugin holds the Pelican panel plugin "echovr-match" (live
// Echo VR match stats per server and for the whole panel), which evr-ptero
// installs into a Pelican panel on its host. The plugin reads the files
// evr-supervisor writes into each server's volume, through Wings.
package panelplugin

import (
	"embed"
	"encoding/json"
	"io/fs"
)

// ID is the plugin's id and folder name.
const ID = "echovr-match"

//go:embed all:echovr-match
var files embed.FS

// Files is the plugin's tree (plugin.json at its root).
func Files() fs.FS {
	sub, err := fs.Sub(files, ID)
	if err != nil {
		panic(err)
	}
	return sub
}

// Version is the version in the embedded plugin.json.
func Version() string {
	b, err := fs.ReadFile(Files(), "plugin.json")
	if err != nil {
		return ""
	}
	var p struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(b, &p)
	return p.Version
}
