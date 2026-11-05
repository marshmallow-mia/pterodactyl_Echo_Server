package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/match"
)

func (a *App) volume(s Server) string { return filepath.Join(a.Cfg.Panel.WingsData, s.Panel.UUID) }

// SessionAPI is a server's match stats setting (on unless switched off).
func (a *App) SessionAPI(s Server) bool { return match.LoadSettings(a.volume(s)).APIOn() }

// SetSessionAPI switches a server's match stats (the game's HTTP API). A
// running server gets the console command "api on|off", so its supervisor
// applies it (idle: restarts the game; in a match: after it); a stopped one
// gets the setting written into its volume for its next start.
func (a *App) SetSessionAPI(ctx context.Context, s Server, on bool) error {
	return a.setSwitch(ctx, s, "api", on, match.SetAPI)
}

// Telemetry reports whether a server's telemetry stream (nevr-runtime's
// play-by-play to the supervisor) is switched on; on by default.
func (a *App) Telemetry(s Server) bool { return match.LoadSettings(a.volume(s)).TelemetryOn() }

// SetTelemetry switches a server's telemetry stream like SetSessionAPI.
func (a *App) SetTelemetry(ctx context.Context, s Server, on bool) error {
	return a.setSwitch(ctx, s, "telemetry", on, match.SetTelemetry)
}

func (a *App) setSwitch(ctx context.Context, s Server, command string, on bool, save func(string, bool) error) error {
	if s.Power == "running" || s.Power == "starting" {
		word := "off"
		if on {
			word = "on"
		}
		return a.Panel().Command(ctx, s.Panel.Identifier, command+" "+word)
	}
	vol := a.volume(s)
	if err := save(vol, on); err != nil {
		return err
	}
	// The volume belongs to Wings' container user; keep the file theirs.
	if fi, err := os.Stat(vol); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			return os.Chown(filepath.Join(vol, match.SettingsFile), int(st.Uid), int(st.Gid))
		}
	}
	return nil
}

// Match is a server's current or last match.
func (a *App) Match(s Server) (match.Match, bool) {
	m, err := match.ReadMatch(a.volume(s))
	return m, err == nil
}

// MatchHistory is a server's finished matches, newest first.
func (a *App) MatchHistory(s Server) ([]match.Summary, error) {
	h, err := match.ReadHistory(a.volume(s))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return h.Matches, err
}
