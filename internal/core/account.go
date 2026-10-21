package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/auth"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
)

var (
	// ErrNoAccount means the servers have no way to sign in to EchoVRCE yet.
	ErrNoAccount = errors.New("the servers have no EchoVRCE sign-in yet")
	// ErrNotSetUp means the panel was not prepared for Echo VR servers.
	ErrNotSetUp = errors.New("the panel is not set up for Echo VR servers yet")
)

// HasAccount reports whether the servers can sign in: a device link (the
// Discord ID is filled in at the next sign-in), or a Discord ID with a password.
func (a *App) HasAccount() bool {
	return a.DeviceLinked() || len(a.Secrets.MissingAccount(false)) == 0
}

// UseDeviceLink links this host at echovrce.com/device, drops a stored
// password (device sign-in needs an account without one) and gives every
// server the link.
func (a *App) UseDeviceLink(ctx context.Context, show func(url, code string, expires time.Time), say func(string)) (auth.Session, error) {
	s, err := a.LinkDevice(ctx, show)
	if err != nil {
		return s, err
	}
	delete(a.Secrets, config.EnvPassword)
	if err := a.Secrets.Save(a.Dir); err != nil {
		return s, err
	}
	say(fmt.Sprintf("Linked as %s (Discord ID %s); %s", s.Username, a.Secrets[config.EnvDiscordID], s.HostSummary()))
	return s, a.ApplyAccount(ctx, say)
}

// UsePassword signs in with a Discord ID and server password, stores them
// only if that works, removes a device link and updates every server.
func (a *App) UsePassword(ctx context.Context, discordID, password string, say func(string)) (auth.Session, error) {
	s, err := a.Auth().Login(ctx, discordID, password)
	if err != nil {
		return s, err
	}
	a.Secrets[config.EnvDiscordID], a.Secrets[config.EnvPassword] = discordID, password
	if err := a.Secrets.Save(a.Dir); err != nil {
		return s, err
	}
	if err := os.Remove(a.devicePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return s, err
	}
	say(fmt.Sprintf("Signed in as %s; %s", s.Username, s.HostSummary()))
	return s, a.ApplyAccount(ctx, say)
}

// ApplyAccount gives every Echo VR server the stored account: the egg
// variables, the device link in its volume (or none), and a restart if it runs.
func (a *App) ApplyAccount(ctx context.Context, say func(string)) error {
	list, err := a.Servers(ctx)
	if err != nil {
		return err
	}
	for _, s := range list {
		if a.DeviceLinked() {
			err = a.installDeviceLink(s.Panel)
		} else {
			err = a.removeDeviceLink(s.Panel)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name(), err)
		}
		if err := a.UpdateServer(ctx, s, func(*instance.Settings) {}, true); err != nil {
			return fmt.Errorf("%s: %w", s.Name(), err)
		}
		if s.Power == "running" {
			say(s.Name() + ": updated, restarting")
		} else {
			say(s.Name() + ": updated")
		}
	}
	return nil
}

// removeDeviceLink deletes a device link from a server's volume, so a
// password sign-in is not shadowed by an old link.
func (a *App) removeDeviceLink(s panel.Server) error {
	err := os.Remove(filepath.Join(a.Cfg.Panel.WingsData, s.UUID, "root", "_local", ".credentials.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
