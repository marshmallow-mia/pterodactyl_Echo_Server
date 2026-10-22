package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
)

// LegacyServer is a server on an older Echo VR egg.
type LegacyServer struct {
	Server  panel.Server
	EggName string
	Port    int
}

// LegacyServers finds servers whose egg looks like an older Echo VR egg
// (name or image mentions echo), other than ours.
func (a *App) LegacyServers(ctx context.Context) ([]LegacyServer, error) {
	p := a.Panel()
	eggs, err := p.Eggs(ctx)
	if err != nil {
		return nil, err
	}
	old := map[int]string{}
	for _, e := range eggs {
		if e.ID == a.Cfg.Panel.EggID {
			continue
		}
		name := strings.ToLower(e.Name)
		images := strings.ToLower(e.DockerImage)
		for _, img := range e.DockerImages {
			images += " " + strings.ToLower(img)
		}
		if strings.Contains(name, "echo") || strings.Contains(images, "echovr") || strings.Contains(images, "echo-arena") {
			old[e.ID] = e.Name
		}
	}
	servers, err := p.Servers(ctx)
	if err != nil {
		return nil, err
	}
	ports := map[int]int{}
	if allocs, err := p.Allocations(ctx, a.Cfg.Panel.NodeID); err == nil {
		for _, al := range allocs {
			ports[al.ID] = al.Port
		}
	}
	var out []LegacyServer
	for _, s := range servers {
		if name, ok := old[s.Egg]; ok {
			out = append(out, LegacyServer{Server: s, EggName: name, Port: ports[s.Allocation]})
		}
	}
	return out, nil
}

// MigrateServers moves servers to the nevr-runtime egg, keeping their
// allocation, owner and limits. The account comes from evr-ptero's secrets.
func (a *App) MigrateServers(ctx context.Context, list []LegacyServer, say func(string)) error {
	if m := a.Secrets.MissingAccount(a.DeviceLinked()); len(m) > 0 {
		return fmt.Errorf("%w (%v missing)", ErrNoAccount, m)
	}
	p := a.Panel()
	for _, ls := range list {
		s := ls.Server
		env := a.Env(a.Cfg.Defaults.Merge(instance.Settings{}), a.Cfg.GameSource)
		if err := p.UpdateStartup(ctx, s, a.Cfg.Panel.EggID, a.Cfg.Image, "evr-supervisor", env, true); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		say(s.Name + ": now on the nevr-runtime egg")
		if a.Cfg.GameSource == "mount" {
			if err := a.attachMountsWhenInstalled(ctx, s); err != nil {
				return err
			}
			say(s.Name + ": shared files attached")
		}
		if a.DeviceLinked() {
			if err := a.installDeviceLink(s); err != nil {
				return fmt.Errorf("%s: device link: %w", s.Name, err)
			}
			say(s.Name + ": device link installed")
		}
		if a.Secrets["PANEL_CLIENT_KEY"] != "" {
			if err := p.Power(ctx, s.Identifier, "restart"); err != nil {
				return fmt.Errorf("%s: restart: %w", s.Name, err)
			}
			say(s.Name + ": restarting")
		} else {
			say(s.Name + ": restart it in the panel")
		}
	}
	return nil
}

// UnhiddenLegacy drops the servers HideLegacy was called for.
func (a *App) UnhiddenLegacy(list []LegacyServer) []LegacyServer {
	var out []LegacyServer
	for _, ls := range list {
		if !slices.Contains(a.Cfg.HiddenLegacy, ls.Server.UUID) {
			out = append(out, ls)
		}
	}
	return out
}

// HideLegacy stops the dashboard from pointing at these old servers. They can
// still be moved from the menu.
func (a *App) HideLegacy(list []LegacyServer) error {
	for _, ls := range list {
		if !slices.Contains(a.Cfg.HiddenLegacy, ls.Server.UUID) {
			a.Cfg.HiddenLegacy = append(a.Cfg.HiddenLegacy, ls.Server.UUID)
		}
	}
	return a.Save()
}

// LegacyCredentials reads the account from an old game directory: config.yaml
// (nevr 4.x) or the serverdb/login URLs in config.json (EchoRelay era).
func LegacyCredentials(gameDir string) (discordID, password, httpKey string) {
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

// LegacyAccount is an account an old installation left on this host.
type LegacyAccount struct {
	DiscordID, Password, HTTPKey string
	Dir                          string // the game directory it was read from
}

// LegacyGameDir is where the v1 guide put the game files (a panel mount).
const LegacyGameDir = "/opt/ready-at-dawn-echo-arena"

// FindLegacyAccount looks for a Discord ID and password in the game folders
// old Echo eggs used: the v1 mount, the folders Wings may mount and the old
// servers' own volumes.
func (a *App) FindLegacyAccount(list []LegacyServer) (LegacyAccount, bool) {
	wc, _, _ := ReadWingsConfig()
	data := a.Cfg.Panel.WingsData
	if wc.System.Data != "" {
		data = wc.System.Data
	}
	var uuids []string
	for _, ls := range list {
		uuids = append(uuids, ls.Server.UUID)
	}
	for _, dir := range legacyDirs(wc.AllowedMounts, data, uuids) {
		if id, pw, key := LegacyCredentials(dir); id != "" && pw != "" {
			return LegacyAccount{DiscordID: id, Password: pw, HTTPKey: key, Dir: dir}, true
		}
	}
	return LegacyAccount{}, false
}

// legacyDirs lists the folders that may hold an old game tree, most likely first.
func legacyDirs(allowedMounts []string, wingsData string, uuids []string) []string {
	dirs := []string{LegacyGameDir}
	for _, m := range allowedMounts {
		if !slices.Contains(dirs, m) {
			dirs = append(dirs, m)
		}
	}
	for _, u := range uuids {
		if u != "" && wingsData != "" {
			dirs = append(dirs, filepath.Join(wingsData, u), filepath.Join(wingsData, u, "ready-at-dawn-echo-arena"))
		}
	}
	return dirs
}

// UseLegacyAccount signs in with an account found by FindLegacyAccount. Its
// HTTP key is kept unless one is set already.
func (a *App) UseLegacyAccount(ctx context.Context, acc LegacyAccount, say func(string)) error {
	if acc.HTTPKey != "" && a.Secrets[config.EnvHTTPKey] == "" {
		a.Secrets[config.EnvHTTPKey] = acc.HTTPKey
	}
	say("Signing in with Discord ID " + acc.DiscordID + " from " + acc.Dir)
	_, err := a.UsePassword(ctx, acc.DiscordID, acc.Password, say)
	return err
}
