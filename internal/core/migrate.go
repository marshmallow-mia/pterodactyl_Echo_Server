package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/game"
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

// How old servers are moved.
const (
	// MigrateRecreate deletes each old server and creates a new one on the
	// same port, owner and name, like a fresh install.
	MigrateRecreate = "recreate"
	// MigrateKeep keeps the panel server (ID, subusers, schedules, backups)
	// and replaces its egg, image, variables and mounts, then reinstalls it.
	MigrateKeep = "keep"
)

// MigrateMode explains a way of moving servers to the people choosing it.
type MigrateMode struct{ Key, Title, Explain string }

// MigrateModes are the ways of moving servers, recommended first.
var MigrateModes = []MigrateMode{
	{MigrateRecreate, "Replace them with new servers (like a fresh install)",
		"Each old server is deleted in the panel and a new one is created on the nevr-runtime egg with the same port, " +
			"name and owner. The new server gets a new ID and address in the panel: subusers, schedules, backups and " +
			"databases of the old server are gone, and so are files in its own folder."},
	{MigrateKeep, "Keep the panel servers, replace what is in them",
		"Each server keeps its ID, address, subusers, schedules, backups and databases. Its egg, image, variables and " +
			"mounts are switched to nevr-runtime and the egg's install step runs again; files in its own folder stay."},
}

// What a migration can delete afterwards.
const (
	RemoveScripts = "scripts"
	RemoveGame    = "game"
	RemovePanel   = "panel" // the old egg and its mounts
)

// Log handling after a migration.
const (
	LogsMove   = "move"
	LogsKeep   = "keep"
	LogsDelete = "delete"
)

// MigrateOptions says how servers are moved and what happens to the old
// installation afterwards.
type MigrateOptions struct {
	Mode   string   // MigrateRecreate or MigrateKeep
	Logs   string   // LogsMove (default), LogsKeep or LogsDelete
	Remove []string // RemoveScripts, RemoveGame, RemovePanel
}

// Validate rejects unknown values and combinations that would lose what was asked to be kept.
func (o MigrateOptions) Validate() error {
	switch o.Mode {
	case MigrateRecreate, MigrateKeep:
	default:
		return fmt.Errorf("choose how the servers are moved: recreate or keep, not %q", o.Mode)
	}
	switch o.Logs {
	case "", LogsMove, LogsKeep, LogsDelete:
	default:
		return fmt.Errorf("logs must be move, keep or delete, not %q", o.Logs)
	}
	for _, r := range o.Remove {
		switch r {
		case RemoveScripts, RemoveGame, RemovePanel:
		default:
			return fmt.Errorf("can remove scripts, game or panel, not %q", r)
		}
		if r == RemoveGame && o.Logs == LogsKeep {
			return errors.New("the old logs are inside the old game folder: move them, or keep the folder")
		}
	}
	return nil
}

// LegacyFiles are the files an old installation left on this host.
type LegacyFiles struct {
	GameDir    string // old game folder, with logs/ ("" if not found)
	LogsSize   int64
	ScriptsDir string // the old scripts mount
	RepoDir    string // folder RepoEntries are deleted from
	// RepoEntries are the v1 checkout's files, or just the scripts folder.
	RepoEntries []string
}

// scriptDirs are the folders deleting the old scripts removes.
func (f LegacyFiles) scriptDirs() []string {
	dirs := []string{f.ScriptsDir}
	if len(f.RepoEntries) > 1 {
		dirs = append(dirs, f.RepoDir) // the whole v1 checkout
	}
	return dirs
}

// cronNote mentions the cron jobs that go with dirs.
func cronNote(dirs []string) string {
	switch n := len(CronLines(dirs)); n {
	case 0:
		return ""
	case 1:
		return " and its cron job"
	default:
		return fmt.Sprintf(" and its %d cron jobs", n)
	}
}

// removeCron deletes the cron jobs that refer to dirs, which are gone now.
func removeCron(dirs []string, say func(string)) error {
	removed, err := RemoveCronLines(dirs)
	for _, l := range removed {
		say("removed the old cron job: " + l)
	}
	if err != nil {
		return fmt.Errorf("removing the old cron jobs: %w", err)
	}
	return nil
}

// LegacyRepoDir is where the v1 guide cloned this repository.
const LegacyRepoDir = "/opt/pterodactyl_Echo_Server"

// v1RepoEntries are the files of a v1 checkout; anything else in it is kept.
var v1RepoEntries = []string{".git", ".github", "Dockerfile", "README.md", "egg-echo.json", "files", "getBinaries.sh", "scripts"}

// FindLegacyFiles looks for the old game folder and scripts in the v1
// locations and the folders Wings may mount, outside evr-ptero's data folder.
func (a *App) FindLegacyFiles() LegacyFiles {
	var f LegacyFiles
	wc, _, _ := ReadWingsConfig()
	data := filepath.Clean(a.Cfg.DataDir)
	candidates := append(legacyDirs(wc.AllowedMounts, "", nil), filepath.Join(LegacyRepoDir, "scripts"))
	for _, dir := range candidates {
		dir = filepath.Clean(dir)
		if dir == data || strings.HasPrefix(dir, data+"/") {
			continue
		}
		if _, err := game.Verify(dir); err == nil && f.GameDir == "" {
			f.GameDir = dir
			f.LogsSize = game.DirSize(filepath.Join(dir, "logs"))
		}
		if _, err := os.Stat(filepath.Join(dir, "start-echo.sh")); err == nil && f.ScriptsDir == "" {
			f.ScriptsDir = dir
			parent := filepath.Dir(dir)
			if _, err := os.Stat(filepath.Join(parent, "getBinaries.sh")); err == nil {
				f.RepoDir, f.RepoEntries = parent, v1RepoEntries
			} else {
				f.RepoDir, f.RepoEntries = parent, []string{filepath.Base(dir)}
			}
		}
	}
	return f
}

// Removal is a part of an old installation that can be deleted after the move.
type Removal struct{ Key, What string }

// Removable lists what of the old installation can be deleted after moving list.
func (a *App) Removable(list []LegacyServer, f LegacyFiles) []Removal {
	var out []Removal
	if f.RepoDir != "" {
		what := "the old scripts " + f.ScriptsDir
		if len(f.RepoEntries) > 1 {
			what = "the old checkout " + f.RepoDir
		}
		out = append(out, Removal{RemoveScripts, what + cronNote(f.scriptDirs())})
	}
	if f.GameDir != "" {
		out = append(out, Removal{RemoveGame, "the old game folder " + f.GameDir + cronNote([]string{f.GameDir})})
	}
	if a.Local() != nil && len(list) > 0 {
		names := map[string]bool{}
		for _, ls := range list {
			names[ls.EggName] = true
		}
		eggs := slices.Sorted(maps.Keys(names))
		out = append(out, Removal{RemovePanel, "the old egg " + strings.Join(eggs, ", ") + " and its mounts in the panel"})
	}
	return out
}

// MigrateServers moves servers from an old Echo egg to the nevr-runtime egg
// (see MigrateRecreate and MigrateKeep), then moves or deletes the old logs
// and deletes what opts.Remove lists. The account comes from evr-ptero's secrets.
func (a *App) MigrateServers(ctx context.Context, list []LegacyServer, f LegacyFiles, opts MigrateOptions, say func(string)) error {
	if m := a.Secrets.MissingAccount(a.DeviceLinked()); len(m) > 0 {
		return fmt.Errorf("%w (%v missing)", ErrNoAccount, m)
	}
	if err := opts.Validate(); err != nil {
		return err
	}
	if opts.Mode == MigrateKeep && slices.Contains(opts.Remove, RemoveGame) && a.Local() == nil {
		return errors.New("kept servers still mount the old game folder; deleting it needs the panel on this host (or recreate the servers)")
	}
	for _, ls := range list {
		var err error
		if opts.Mode == MigrateRecreate {
			err = a.recreateServer(ctx, ls, say)
		} else {
			err = a.replaceInServer(ctx, ls, say)
		}
		if err != nil {
			return err
		}
	}
	return a.cleanupLegacy(ctx, list, f, opts, say)
}

// recreateServer deletes an old server and creates a new one on its port.
func (a *App) recreateServer(ctx context.Context, ls LegacyServer, say func(string)) error {
	s, p := ls.Server, a.Panel()
	if err := p.DeleteServer(ctx, s.ID, true); err != nil {
		return fmt.Errorf("%s: deleting the old server: %w", s.Name, err)
	}
	say(fmt.Sprintf("%s: old server %s deleted", s.Name, s.Identifier))
	ns, err := p.CreateServer(ctx, a.serverSpec(s.Name, s.User, s.Allocation, a.Cfg.Defaults.Merge(instance.Settings{})))
	if err != nil {
		return fmt.Errorf("%s: creating the new server on port %d: %w", s.Name, ls.Port, err)
	}
	say(fmt.Sprintf("%s: new server %s on port %d", s.Name, ns.Identifier, ls.Port))
	return a.startWhenInstalled(ctx, ns, say)
}

// replaceInServer keeps the panel server and replaces what is in it.
func (a *App) replaceInServer(ctx context.Context, ls LegacyServer, say func(string)) error {
	s, p := ls.Server, a.Panel()
	env := a.Env(a.Cfg.Defaults.Merge(instance.Settings{}), a.Cfg.GameSource)
	if err := p.UpdateStartup(ctx, s, a.Cfg.Panel.EggID, a.Cfg.Image, "evr-supervisor", env, false); err != nil {
		return fmt.Errorf("%s: %w", s.Name, err)
	}
	say(s.Name + ": now on the nevr-runtime egg")
	if l := a.Local(); l != nil {
		if err := l.DetachMounts(ctx, s.ID, a.Cfg.Panel.MountIDs); err != nil {
			return fmt.Errorf("%s: removing the old mounts: %w", s.Name, err)
		}
		say(s.Name + ": old mounts removed")
	} else {
		say(s.Name + ": its old mounts stay attached; remove them in the panel's server settings")
	}
	if err := p.Reinstall(ctx, s.ID); err != nil {
		return fmt.Errorf("%s: reinstall: %w", s.Name, err)
	}
	say(s.Name + ": reinstalling with the nevr-runtime egg")
	return a.startWhenInstalled(ctx, s, say)
}

// cleanupLegacy moves or deletes the old logs, then removes what opts lists.
func (a *App) cleanupLegacy(ctx context.Context, list []LegacyServer, f LegacyFiles, opts MigrateOptions, say func(string)) error {
	if f.GameDir != "" {
		switch opts.Logs {
		case "", LogsMove:
			dst := filepath.Join(a.Cfg.DataDir, game.V1LogsDir)
			n, err := game.MoveLogs(f.GameDir, dst)
			if err != nil {
				return fmt.Errorf("moving the old logs: %w", err)
			}
			if n > 0 {
				say("moved the old logs to " + dst)
			}
		case LogsDelete:
			if err := os.RemoveAll(filepath.Join(f.GameDir, "logs")); err != nil {
				return err
			}
			say("deleted the old logs")
		}
	}
	remove := map[string]bool{}
	for _, r := range opts.Remove {
		remove[r] = true
	}
	if l := a.Local(); remove[RemovePanel] && l != nil {
		var eggs []int
		for _, ls := range list {
			if !slices.Contains(eggs, ls.Server.Egg) {
				eggs = append(eggs, ls.Server.Egg)
			}
		}
		var sources []string
		for _, d := range []string{f.GameDir, f.ScriptsDir} {
			if d != "" {
				sources = append(sources, d)
			}
		}
		gone, mounts, err := l.RemoveLegacy(ctx, eggs, sources)
		if err != nil {
			return fmt.Errorf("removing the old egg: %w", err)
		}
		for _, e := range gone {
			say("deleted the old egg " + e)
		}
		for _, m := range mounts {
			say("deleted the old mount " + m)
		}
	}
	if remove[RemoveGame] && f.GameDir != "" {
		if err := os.RemoveAll(f.GameDir); err != nil {
			return fmt.Errorf("deleting the old game folder: %w", err)
		}
		say("deleted the old game folder " + f.GameDir)
		if err := removeCron([]string{f.GameDir}, say); err != nil {
			return err
		}
	}
	if remove[RemoveScripts] && f.RepoDir != "" {
		if err := game.RemoveEntries(f.RepoDir, f.RepoEntries); err != nil {
			return fmt.Errorf("deleting the old scripts: %w", err)
		}
		say("deleted the old scripts in " + f.RepoDir)
		if err := removeCron(f.scriptDirs(), say); err != nil {
			return err
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
