package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
	panelplugin "github.com/marshmallow-mia/pterodactyl_Echo_Server/panel-plugin"
)

// syncPlugin installs or updates the panel's Echo VR match plugin when the
// panel runs on this host and has a plugin system. A failure is reported,
// not fatal: servers run without it.
func (a *App) syncPlugin(ctx context.Context, say func(string, string)) {
	if a.local == nil || !a.local.SupportsPlugins() {
		return
	}
	if a.Cfg.Panel.NoPlugin {
		say("Plugin", panelplugin.ID+" left out (removed with \"evr-ptero panel plugin remove\"; \"panel plugin install\" brings it back)")
		return
	}
	res, err := a.local.InstallPlugin(ctx, panelplugin.ID, panelplugin.Files())
	if err != nil {
		say("Plugin", fmt.Sprintf("%s %s: %v (the servers work without it)", panelplugin.ID, panelplugin.Version(), err))
		return
	}
	say("Plugin", pluginLine(res))
}

func pluginLine(res panel.PluginResult) string {
	switch res.Action {
	case "updated":
		return fmt.Sprintf("%s updated %s → %s (match stats in the panel)", panelplugin.ID, res.Old, res.Version)
	case "up to date":
		return fmt.Sprintf("%s %s is up to date", panelplugin.ID, res.Version)
	}
	return fmt.Sprintf("%s %s installed: each Echo VR server has a Match page, the admin area \"Echo VR matches\"", panelplugin.ID, res.Version)
}

// InstallPlugin installs or updates the plugin on this host's panel.
func (a *App) InstallPlugin(ctx context.Context) (string, error) {
	if a.local == nil {
		return "", errors.New("no panel on this host: import the plugin zip in the panel's Admin → Plugins instead")
	}
	res, err := a.local.InstallPlugin(ctx, panelplugin.ID, panelplugin.Files())
	if err != nil {
		return "", err
	}
	if a.Cfg.Panel.NoPlugin {
		a.Cfg.Panel.NoPlugin = false
		if err := a.Save(); err != nil {
			return "", err
		}
	}
	return pluginLine(res), nil
}

// RemovePlugin uninstalls the plugin and keeps setup from bringing it back.
func (a *App) RemovePlugin(ctx context.Context) error {
	if a.local == nil {
		return errors.New("no panel on this host")
	}
	if err := a.local.UninstallPlugin(ctx, panelplugin.ID); err != nil {
		return err
	}
	a.Cfg.Panel.NoPlugin = true
	return a.Save()
}

// PluginStatus is the plugin's installed version and panel status.
func (a *App) PluginStatus() (installed, status, bundled string) {
	bundled = panelplugin.Version()
	if a.local == nil {
		return "", "", bundled
	}
	installed, status = a.local.PluginVersion(panelplugin.ID)
	return installed, status, bundled
}
