package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/update"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// Policy is the runtime policy of the config.
func (a *App) Policy() update.Policy {
	return update.Policy{Channel: a.Cfg.Runtime.Channel, Pinned: a.Cfg.Runtime.Pinned}
}

// Channels returns nEVR's channels (name → version), stable first in names.
func (a *App) Channels(ctx context.Context) (names []string, versions map[string]string, err error) {
	versions, err = (&nevr.Client{}).Channels(ctx)
	return nevr.ChannelNames(versions), versions, err
}

// SetRuntimePolicy saves who updates the runtime and the channel it follows.
// An empty updater keeps the current one. A channel nEVR's manifest doesn't
// list is refused (when the manifest can be read); a pin needs evr-ptero
// updating. ApplyRuntimePolicy gives the servers the change.
func (a *App) SetRuntimePolicy(ctx context.Context, updater, channel, pinned string) error {
	r := a.Cfg.Runtime
	if updater != "" {
		r.Updater = updater
	}
	if channel != "" {
		r.Channel, r.Pinned = nevr.ChannelName(channel), pinned
	}
	if r.Channel == "pinned" && r.Updater == pcfg.UpdaterNevr {
		if updater != "" {
			return fmt.Errorf("nEVR can't update itself while the runtime is pinned to %s: pick a channel first", r.Pinned)
		}
		r.Updater = pcfg.UpdaterHost // pinning hands updating to evr-ptero
	}
	if r.Channel != "pinned" {
		if names, chs, err := a.Channels(ctx); err == nil {
			if _, ok := chs[r.Channel]; !ok {
				return fmt.Errorf("nEVR has no channel %q (it has %s)", r.Channel, strings.Join(names, ", "))
			}
		}
	}
	old := a.Cfg.Runtime
	a.Cfg.Runtime = r
	if err := a.Save(); err != nil {
		a.Cfg.Runtime = old
		return err
	}
	return nil
}

// RuntimePolicyText describes who updates the runtime and from where.
func (a *App) RuntimePolicyText(versions map[string]string) string {
	r := a.Cfg.Runtime
	from := "channel " + r.Channel
	if r.Channel == "pinned" {
		from = "pinned to " + r.Pinned
	} else if v := versions[r.Channel]; v != "" {
		from += ", now " + v
	}
	if r.Updater == pcfg.UpdaterNevr {
		return "nEVR updates itself (" + from + ")"
	}
	return "evr-ptero updates it (" + from + ")"
}

// SelfUpdateNote warns when nEVR is to update itself but its channel's
// release can't yet (before nevr.UpdateConfigMin).
func (a *App) SelfUpdateNote(versions map[string]string) string {
	r := a.Cfg.Runtime
	if r.Updater != pcfg.UpdaterNevr {
		return ""
	}
	if v := versions[r.Channel]; v != "" && !nevr.UpdateConfigurable(v) {
		return fmt.Sprintf("nEVR %s on %s can't be left to update itself yet (that needs %s); evr-ptero keeps it current until it can", v, r.Channel, nevr.UpdateConfigMin)
	}
	return ""
}

// ApplyRuntimePolicy gives every Echo VR server whose environment differs the
// runtime policy's (who updates, the channel). The panel hands a server its
// environment when it starts, so with restart an idle server restarts now and
// one in a match once the match has ended; without, each takes it at its next
// start.
func (a *App) ApplyRuntimePolicy(ctx context.Context, restart bool, say func(string)) error {
	list, err := a.Servers(ctx)
	if err != nil {
		return err
	}
	want := a.Cfg.Runtime.SelfUpdateEnv()
	var inMatch []Server
	for _, s := range list {
		env, same := s.Panel.Env(), true
		for k, v := range want {
			same = same && env[k] == v
		}
		if same {
			continue
		}
		if err := a.UpdateServer(ctx, s, func(*instance.Settings) {}, false); err != nil {
			return fmt.Errorf("%s: %w", s.Name(), err)
		}
		switch {
		case !restart || s.Power != "running" || a.Secrets[pcfg.SecretClientKey] == "":
			say(s.Name() + ": updated; takes it at its next start")
		case s.Busy():
			inMatch = append(inMatch, s)
			say(s.Name() + ": updated; in a match, restarts when it ends")
		default:
			if err := a.Panel().Power(ctx, s.Panel.Identifier, "restart"); err != nil {
				return fmt.Errorf("%s: %w", s.Name(), err)
			}
			say(s.Name() + ": updated, restarting")
		}
	}
	return a.restartAfterMatch(ctx, inMatch, say)
}
