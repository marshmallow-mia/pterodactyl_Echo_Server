// Package egg defines the Echo VR egg once and renders it for Pterodactyl
// (PTDL_v2 JSON) and Pelican (PLCN_v3 YAML).
package egg

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Name is the egg's name in the panel; evr-ptero finds its egg by it.
const Name = "Echo VR (nevr-runtime)"

// UUID is stable so Pelican updates the same egg on re-import.
const UUID = "6a3c1e5e-8f4b-4d2a-9c71-3e0f5b7d2a10"

// ReadyMarker is printed by evr-supervisor once the server is registered.
const ReadyMarker = "[evr] READY"

// Variable is one egg variable.
type Variable struct {
	Name, Description, Env, Default string
	Viewable, Editable              bool
	Rules                           []string
}

// Spec is the egg content that varies between releases.
type Spec struct {
	Image   string // docker image for servers and the installer
	Version string // evr-ptero version that generated it
}

// Variables lists the egg's variables; names avoid Pelican's reserved ones.
func Variables() []Variable {
	return []Variable{
		{"Discord ID", "Discord user ID of the EchoVRCE host account.", "NEVR_DISCORD_ID", "", false, false, []string{"required", "string", "regex:/^[0-9]{5,25}$/"}},
		{"Server password", "Password of the EchoVRCE host account. Only admins can see or change it. Leave empty when the node uses a device link (evr-ptero account link).", "NEVR_PASSWORD", "", false, false, []string{"nullable", "string", "max:200"}},
		{"Backend HTTP key", "http_key for the EchoVRCE backend (g.echovrce.com:7350).", "NEVR_HTTP_KEY", "", false, false, []string{"required", "string", "max:200"}},
		{"Regions", "Comma-separated regions this server offers itself for, e.g. euw. Empty uses the backend's default.", "NEVR_REGIONS", "", true, true, []string{"nullable", "string", "max:200", "regex:/^[A-Za-z0-9_,-]*$/"}},
		{"Guilds", "Comma-separated guild IDs to host for. Empty hosts for every guild the account may host.", "NEVR_GUILDS", "", false, false, []string{"nullable", "string", "max:2000"}},
		{"Tick rate", "Server tick rate in Hz (10-1000).", "NEVR_TICK_RATE", "120", true, false, []string{"required", "integer", "between:10,1000"}},
		{"Empty server TTL", "Seconds an empty server waits before it returns to the lobby (0 = immediately).", "NEVR_EMPTY_TTL", "0", true, false, []string{"required", "integer", "between:0,86400"}},
		{"nevr-runtime version", `"current" follows the node's current runtime (updated by evr-ptero); or pin a tag like v5.4.2.`, "NEVR_VERSION", "current", true, false, []string{"required", "string", "regex:/^(current)?(v[0-9]+\\.[0-9]+\\.[0-9]+[0-9A-Za-z.-]*)?$/"}},
		{"Publisher lock", "Game publisher lock; the EchoVRCE backend expects echovrce.", "NEVR_PUBLISHER_LOCK", "echovrce", false, false, []string{"required", "string", "max:50"}},
		{"Game files", `"mount" uses the node's shared read-only files (/srv/echovr, set up by evr-ptero); "download" installs a private copy (about 5 GB) into this server.`, "GAME_SOURCE", "mount", false, false, []string{"required", "string", "in:mount,download"}},
		{"Console output", `"default" hides the runtime's noisy diagnostics, "all" shows everything.`, "EVR_LOG", "default", true, true, []string{"required", "string", "in:default,all"}},
	}
}

// InstallScript runs in the installer container (our image, as root).
const InstallScript = `#!/bin/bash
# Echo VR (nevr-runtime) installer. With GAME_SOURCE=mount nothing is copied:
# the node provides the game and runtime read-only at /opt/echovr.
set -euo pipefail
if [ "${GAME_SOURCE:-mount}" != "download" ]; then
  echo "Game files come from the node's shared mount (/opt/echovr). Nothing to install."
  exit 0
fi
evr-ptero files install --dir /mnt/server/echovr --runtime "${NEVR_VERSION:-current}"
`

const startup = "evr-supervisor"

const description = "Echo VR dedicated server with nevr-runtime under Wine. Managed with evr-ptero " +
	"(github.com/marshmallow-mia/pterodactyl_Echo_Server). Uses the node's shared game files by default; " +
	"the game port must be forwarded 1:1 (same external and internal UDP port)."

// PTDL renders the Pterodactyl egg (PTDL_v2 JSON).
func PTDL(s Spec) ([]byte, error) {
	type variable struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		EnvVariable  string `json:"env_variable"`
		DefaultValue string `json:"default_value"`
		UserViewable bool   `json:"user_viewable"`
		UserEditable bool   `json:"user_editable"`
		Rules        string `json:"rules"`
		FieldType    string `json:"field_type"`
	}
	vars := []variable{}
	for _, v := range Variables() {
		vars = append(vars, variable{v.Name, v.Description, v.Env, v.Default, v.Viewable, v.Editable, strings.Join(v.Rules, "|"), "text"})
	}
	jsonString := func(v any) string {
		b, _ := json.MarshalIndent(v, "", "    ")
		return string(b)
	}
	egg := map[string]any{
		"_comment":      "DO NOT EDIT: FILE GENERATED AUTOMATICALLY BY PTERODACTYL PANEL - PTERODACTYL.IO",
		"meta":          map[string]any{"version": "PTDL_v2", "update_url": nil},
		"exported_at":   time.Now().UTC().Format(time.RFC3339),
		"name":          Name,
		"author":        "echo@mia-hentschel.de",
		"description":   description + " Generated by evr-ptero " + s.Version + ".",
		"features":      nil,
		"docker_images": map[string]string{"Echo VR nevr": s.Image},
		"file_denylist": []string{},
		"startup":       startup,
		"config": map[string]string{
			"files":   "{}",
			"startup": jsonString(map[string]any{"done": ReadyMarker}),
			"logs":    "{}",
			"stop":    "^C",
		},
		"scripts": map[string]any{"installation": map[string]any{
			"script": InstallScript, "container": s.Image, "entrypoint": "bash",
		}},
		"variables": vars,
	}
	return json.MarshalIndent(egg, "", "    ")
}

// PLCN renders the Pelican egg (PLCN_v3 YAML).
func PLCN(s Spec) ([]byte, error) {
	type variable struct {
		Sort         int      `yaml:"sort"`
		Name         string   `yaml:"name"`
		Description  string   `yaml:"description"`
		EnvVariable  string   `yaml:"env_variable"`
		DefaultValue string   `yaml:"default_value"`
		UserViewable bool     `yaml:"user_viewable"`
		UserEditable bool     `yaml:"user_editable"`
		Rules        []string `yaml:"rules"`
	}
	var vars []variable
	for i, v := range Variables() {
		vars = append(vars, variable{i + 1, v.Name, v.Description, v.Env, v.Default, v.Viewable, v.Editable, v.Rules})
	}
	egg := map[string]any{
		"_comment":         "DO NOT EDIT: FILE GENERATED AUTOMATICALLY BY PANEL",
		"meta":             map[string]any{"version": "PLCN_v3", "update_url": nil},
		"exported_at":      time.Now().UTC().Format(time.RFC3339),
		"name":             Name,
		"author":           "echo@mia-hentschel.de",
		"uuid":             UUID,
		"description":      description + " Generated by evr-ptero " + s.Version + ".",
		"tags":             []string{"echovr", "wine"},
		"features":         []string{},
		"docker_images":    map[string]string{"Echo VR nevr": s.Image},
		"file_denylist":    []string{},
		"startup_commands": map[string]string{"Default": startup},
		"config": map[string]any{
			"files":   map[string]any{},
			"startup": map[string]any{"done": []string{ReadyMarker}},
			"logs":    map[string]any{},
			"stop":    "^C",
		},
		"scripts": map[string]any{"installation": map[string]any{
			"script": InstallScript, "container": s.Image, "entrypoint": "bash",
		}},
		"variables": vars,
	}
	return yaml.Marshal(egg)
}

// Render returns the egg in the given format ("ptdl" or "plcn").
func Render(format string, s Spec) ([]byte, error) {
	switch format {
	case "ptdl", "json", "pterodactyl":
		return PTDL(s)
	case "plcn", "yaml", "pelican":
		return PLCN(s)
	}
	return nil, fmt.Errorf("unknown egg format %q (use ptdl or plcn)", format)
}
