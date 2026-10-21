package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
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
			if err := a.attachMounts(ctx, s); err != nil {
				return fmt.Errorf("%s: %w", s.Name, err)
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
