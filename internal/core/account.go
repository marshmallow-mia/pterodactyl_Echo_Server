package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	// ErrSignInByCode means no password is set, so there is no account to
	// check from the host: each server signs in on its own.
	ErrSignInByCode = errors.New("no server password set: " + SignInCodeNote)
	// ErrDeviceLinkOff means a device link was asked for without the switch.
	ErrDeviceLinkOff = errors.New("the host-wide device link is off (nevr-runtime 5.x only; from v6.9.0 on each server shows its own sign-in code); run with --device-link or " + DeviceLinkEnv + "=1 to link this host anyway")
)

// SignInCodeNote explains the sign-in without a password.
const SignInCodeNote = "each server prints a sign-in code on its console in the panel the first time it starts (nevr-runtime v6.9.0 and later); approve it at echovrce.com/device with the host account, and the server keeps the sign-in"

// DeviceLinkFlow turns the old host-wide echovrce.com/device link on
// (--device-link, hidden): only nevr-runtime 5.x servers use it. From v6.9.0
// on a server without a password signs in by its own console code, so a new
// link is offered or made only with this switch or DeviceLinkEnv. A link that
// is already stored keeps working.
var DeviceLinkFlow bool

// DeviceLinkEnv turns the device link on like --device-link ("1" or "true").
const DeviceLinkEnv = "EVR_DEVICE_LINK"

// DeviceLinkEnabled reports whether new device links are offered.
func DeviceLinkEnabled() bool {
	if DeviceLinkFlow {
		return true
	}
	on, _ := strconv.ParseBool(os.Getenv(DeviceLinkEnv))
	return on
}

// HasAccount reports whether the servers can sign in: with a complete
// password sign-in, or without a password (console code, device link).
func (a *App) HasAccount() bool { return len(a.Secrets.Missing()) == 0 }

// SignsInByCode reports whether the servers sign in by their console code:
// no password and no host-wide device link.
func (a *App) SignsInByCode() bool { return a.Secrets[config.EnvPassword] == "" && !a.DeviceLinked() }

// UseSignInCode removes the password (and a host-wide device link), so each
// server signs in by the code it prints on its console, and updates every
// server.
func (a *App) UseSignInCode(ctx context.Context, say func(string)) error {
	had := !a.SignsInByCode()
	delete(a.Secrets, config.EnvPassword)
	delete(a.Secrets, config.EnvDiscordID)
	if err := a.Secrets.Save(a.Dir); err != nil {
		return err
	}
	if err := os.Remove(a.devicePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if had {
		// The sign-in a password or link left in a server would stand in for
		// the code; one a code made is kept.
		list, err := a.Servers(ctx)
		if err != nil {
			return err
		}
		for _, s := range list {
			if err := a.removeDeviceLink(s.Panel); err != nil {
				return fmt.Errorf("%s: %w", s.Name(), err)
			}
		}
	}
	say("No password stored: " + SignInCodeNote)
	return a.ApplyAccount(ctx, say)
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
// With the console code a server's own sign-in in its nEVR root is kept.
func (a *App) ApplyAccount(ctx context.Context, say func(string)) error {
	list, err := a.Servers(ctx)
	if err != nil {
		return err
	}
	for _, s := range list {
		switch {
		case a.DeviceLinked():
			err = a.installDeviceLink(s.Panel)
		case a.SignsInByCode():
			err = a.removeLegacyLink(s.Panel)
		default:
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

// removeLegacyLink deletes an old device link from a server's _local only.
func (a *App) removeLegacyLink(s panel.Server) error {
	p := filepath.Join(a.Cfg.Panel.WingsData, s.UUID, "root", "_local", ".credentials.json")
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// removeDeviceLink deletes a device link from a server's volume, so a
// password sign-in is not shadowed by an old link.
func (a *App) removeDeviceLink(s panel.Server) error {
	vol := filepath.Join(a.Cfg.Panel.WingsData, s.UUID)
	for _, p := range []string{filepath.Join(vol, "root", "_local", ".credentials.json"), rootCredentials(vol)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
