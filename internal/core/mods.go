package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/mods"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/status"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// LockName is a server's mods lock in its volume (/home/container), where
// evr-supervisor reads it; the mod versions it names are copied beside it into
// mods/store, as the volume is all a server sees of this host.
const LockName = "mods.lock.json"

// ErrNoVolume: mods are written into the server volumes, which needs Wings on
// this host.
var ErrNoVolume = errors.New("the server volume is not on this host (mods need Wings here)")

// ModStore keeps the downloaded mods.
func (a *App) ModStore() mods.Store { return mods.Store{Dir: a.Cfg.ModsDir()} }

// ModHost resolves, downloads and rolls out the servers' mods from the
// launcher's catalogues (and the dev codes in secrets.env).
func (a *App) ModHost(log func(string)) mods.Host {
	return mods.Host{Source: mods.Source{DevCodes: a.Secrets.DevCodes()}, Store: a.ModStore(), Fleet: &modFleet{a: a}, Log: log}
}

// modServers remembers which panel server a name is, for the fleet.
var modServers sync.Map // name → uuid

func (a *App) volume(name string) (string, error) {
	uuid, ok := modServers.Load(name)
	if !ok {
		return "", fmt.Errorf("no Echo VR server %q", name)
	}
	vol := filepath.Join(a.Cfg.Panel.WingsData, uuid.(string))
	if _, err := os.Stat(vol); err != nil {
		return "", fmt.Errorf("%s: %w", name, ErrNoVolume)
	}
	return vol, nil
}

// ModServers are the panel's Echo VR servers with their mods (defaults and own).
func (a *App) ModServers(ctx context.Context) ([]mods.ServerMods, error) {
	servers, err := a.Servers(ctx)
	if err != nil {
		return nil, err
	}
	cur := a.Store().Current()
	var out []mods.ServerMods
	for _, s := range servers {
		set := a.ModSettings(s)
		rt := SettingsFromEnv(s.Panel.Env()).Runtime
		if rt == "" || rt == "current" {
			rt = cur
		}
		modServers.Store(s.Name(), s.Panel.UUID)
		out = append(out, mods.ServerMods{Name: s.Name(), Specs: set.Mods, LocalPlugins: set.Local(), Runtime: rt})
	}
	return out, nil
}

// ModSettings are a server's mods: the defaults' with its own over them.
func (a *App) ModSettings(s Server) instance.Settings {
	own := a.Cfg.ServerMods[s.Panel.UUID]
	return a.Cfg.Defaults.Merge(instance.Settings{Mods: own.Mods, LocalPlugins: own.LocalPlugins})
}

// SyncMods writes each server's lock after an edit: what it names changes, the
// rest stays at the versions the servers run (new versions come with updates).
// Servers place their lock at their next start, after their match.
func (a *App) SyncMods(ctx context.Context, log func(string)) ([]mods.Planned, []string, error) {
	servers, err := a.ModServers(ctx)
	if err != nil {
		return nil, nil, err
	}
	h := a.ModHost(log)
	plans, _, err := h.Plan(ctx, servers, true)
	if err != nil {
		return nil, nil, err
	}
	if err := h.Download(ctx, plans); err != nil {
		return plans, nil, err
	}
	changed, err := h.Write(plans)
	return plans, changed, err
}

// ModStatuses returns the status of every running server, by panel name.
func (a *App) ModStatuses(ctx context.Context) ([]status.Status, error) {
	servers, err := a.Servers(ctx)
	if err != nil {
		return nil, err
	}
	var out []status.Status
	for _, s := range servers {
		if s.Status == nil || s.Power == "offline" {
			continue
		}
		st := *s.Status
		st.Instance = s.Name()
		if s.State() == "starting" {
			st.State = status.StateStarting
		}
		out = append(out, st)
	}
	return out, nil
}

// modFleet is the panel's servers as a mod rollout sees them.
type modFleet struct{ a *App }

func (f *modFleet) Statuses(ctx context.Context) ([]status.Status, error) {
	return f.a.ModStatuses(ctx)
}

func (f *modFleet) ReadLock(server string) (mods.Lock, error) {
	vol, err := f.a.volume(server)
	if err != nil {
		return mods.Lock{}, err
	}
	root, err := os.OpenRoot(vol)
	if err != nil {
		return mods.Lock{}, err
	}
	defer root.Close()
	b, err := root.ReadFile(LockName)
	var l mods.Lock
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	return l, json.Unmarshal(b, &l)
}

// WriteLock copies the lock's mod versions into the volume's mods/store (and
// drops the ones it no longer names), then writes the lock, owned by the
// volume's owner. Everything goes through an os.Root of the volume: a link a
// server put there can't lead outside it.
func (f *modFleet) WriteLock(server string, l mods.Lock) error {
	vol, err := f.a.volume(server)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(vol)
	if err != nil {
		return err
	}
	defer root.Close()
	o := ownerOf(vol)
	keep := map[string]bool{}
	for _, m := range l.Mods {
		if m.Shipped {
			continue
		}
		keep[m.ID+"/"+m.Version] = true
		if err := copyVersion(f.a.ModStore(), root, m.ID, m.Version, o); err != nil {
			return fmt.Errorf("%s %s: %w", m.ID, m.Version, err)
		}
	}
	// Versions the lock no longer names go.
	if ids, err := readDirNames(root, volStore); err == nil {
		for _, id := range ids {
			vers, _ := readDirNames(root, path.Join(volStore, id))
			for _, v := range vers {
				if !keep[id+"/"+v] {
					_ = root.RemoveAll(path.Join(volStore, id, v))
				}
			}
			_ = root.Remove(path.Join(volStore, id)) // only when empty
		}
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := root.WriteFile(LockName+".tmp", b, 0o644); err != nil {
		return err
	}
	o.chown(root, LockName+".tmp")
	return root.Rename(LockName+".tmp", LockName)
}

// Restart asks an idle server's supervisor to start the game again (its
// control file), which places the lock.
func (f *modFleet) Restart(_ context.Context, server string) error {
	vol, err := f.a.volume(server)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(vol)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.WriteFile("control", []byte("restart\n"), 0o644); err != nil {
		return err
	}
	ownerOf(vol).chown(root, "control")
	return nil
}

// volStore is where a server's copy of its mod versions lives in its volume
// (evr-supervisor's fallback store, $HOME/mods/store).
const volStore = "mods/store"

type owner struct {
	uid, gid int
	ok       bool
}

func ownerOf(p string) owner {
	if fi, err := os.Stat(p); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			return owner{int(st.Uid), int(st.Gid), true}
		}
	}
	return owner{}
}

func (o owner) chown(root *os.Root, name string) {
	if o.ok {
		_ = root.Lchown(name, o.uid, o.gid)
	}
}

func readDirNames(root *os.Root, dir string) ([]string, error) {
	d, err := root.Open(dir)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	ents, err := d.ReadDir(-1)
	var out []string
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out, err
}

// copyVersion copies a stored mod version (its files and manifest) from the
// host's store into the volume's unless the volume has it with the same
// manifest. The server checks every file against it again when it places them.
func copyVersion(src mods.Store, root *os.Root, id, ver string, o owner) error {
	in, err := src.Get(id, ver)
	if err != nil {
		return fmt.Errorf("not downloaded: %w", err)
	}
	if in.Bad != "" {
		return fmt.Errorf("marked bad: %s", in.Bad)
	}
	to := path.Join(volStore, id, ver)
	if b, err := root.ReadFile(path.Join(to, "manifest.json")); err == nil {
		var have mods.Installed
		if json.Unmarshal(b, &have) == nil && sameFiles(have.Files, in.Files) {
			return nil
		}
	}
	from := filepath.Join(src.Dir, id, ver)
	tmp := to + ".new"
	_ = root.RemoveAll(tmp)
	if err := root.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	err = filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		t := path.Join(tmp, filepath.ToSlash(rel))
		switch {
		case d.IsDir():
			if err := root.MkdirAll(t, 0o755); err != nil {
				return err
			}
		case d.Type().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if err := root.WriteFile(t, b, 0o644); err != nil {
				return err
			}
		default:
			return nil
		}
		o.chown(root, t)
		return nil
	})
	if err != nil {
		_ = root.RemoveAll(tmp)
		return err
	}
	_ = root.RemoveAll(to)
	for _, d := range []string{"mods", volStore, path.Join(volStore, id)} {
		o.chown(root, d)
	}
	return root.Rename(tmp, to)
}

func sameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// ModRow is one mod a server runs, as `mods ls` and the TUI show it.
type ModRow struct {
	Server, ID, Version, State string
}

// ModRows reads each server's lock (only: one server) and what its last start
// made of it.
func (a *App) ModRows(ctx context.Context, only string) ([]ModRow, error) {
	servers, err := a.Servers(ctx)
	if err != nil {
		return nil, err
	}
	var rows []ModRow
	for _, s := range servers {
		if only != "" && s.Name() != only {
			continue
		}
		modServers.Store(s.Name(), s.Panel.UUID)
		lock, err := (&modFleet{a: a}).ReadLock(s.Name())
		if err != nil && !errors.Is(err, ErrNoVolume) {
			return nil, err
		}
		state := map[string]string{}
		if s.Status != nil {
			for _, m := range s.Status.Mods {
				switch {
				case m.Error != "":
					state[m.ID+" "+m.Version] = "failed: " + m.Error
				case m.Loaded:
					state[m.ID+" "+m.Version] = "loaded"
				default:
					state[m.ID+" "+m.Version] = "placed"
				}
			}
			for _, sk := range s.Status.ModsSkipped {
				rows = append(rows, ModRow{s.Name(), "-", "-", "left out: " + sk})
			}
		}
		for _, m := range lock.Mods {
			st := state[m.ID+" "+m.Version]
			if st == "" {
				st = "from the next start"
			}
			rows = append(rows, ModRow{s.Name(), m.ID, m.Version, st})
		}
		if lock.LocalPlugins {
			rows = append(rows, ModRow{s.Name(), "(local DLLs)", "-", "on"})
		}
	}
	return rows, nil
}

// SpecList is a mod list in one line.
func SpecList(ss []mods.Spec) string {
	if len(ss) == 0 {
		return "none"
	}
	var out []string
	for _, s := range ss {
		t := s.ID
		if s.Version != "" {
			t += "@" + s.Version
		}
		if s.Enabled != nil && !*s.Enabled {
			t += " (off)"
		}
		if len(s.Args) > 0 {
			var kv []string
			for k, v := range s.Args {
				kv = append(kv, k+"="+v)
			}
			sort.Strings(kv)
			t += " [" + strings.Join(kv, " ") + "]"
		}
		out = append(out, t)
	}
	return strings.Join(out, ", ")
}

// editMods runs edit on the defaults (server "") or one server's own mods and
// saves; a refused edit leaves them as they were.
func (a *App) editMods(ctx context.Context, server string, edit func(list *[]mods.Spec, local **bool, inDefaults func(string) bool) error) error {
	inDefaults := func(id string) bool {
		return slices.ContainsFunc(a.Cfg.Defaults.Mods, func(m mods.Spec) bool { return m.ID == id })
	}
	if server == "" {
		before, local := slices.Clone(a.Cfg.Defaults.Mods), a.Cfg.Defaults.LocalPlugins
		err := edit(&a.Cfg.Defaults.Mods, &a.Cfg.Defaults.LocalPlugins, inDefaults)
		if err == nil {
			err = a.Save()
		}
		if err != nil {
			a.Cfg.Defaults.Mods, a.Cfg.Defaults.LocalPlugins = before, local
		}
		return err
	}
	s, err := a.Find(ctx, server)
	if err != nil {
		return err
	}
	if a.Cfg.ServerMods == nil {
		a.Cfg.ServerMods = map[string]pcfg.OwnMods{}
	}
	before, had := a.Cfg.ServerMods[s.Panel.UUID]
	own := before
	own.Mods = slices.Clone(before.Mods)
	own.Name = s.Name()
	err = edit(&own.Mods, &own.LocalPlugins, inDefaults)
	if err == nil {
		if len(own.Mods) == 0 && own.LocalPlugins == nil {
			delete(a.Cfg.ServerMods, s.Panel.UUID)
		} else {
			a.Cfg.ServerMods[s.Panel.UUID] = own
		}
		if err = a.Save(); err != nil {
			delete(a.Cfg.ServerMods, s.Panel.UUID)
			if had {
				a.Cfg.ServerMods[s.Panel.UUID] = before
			}
		}
	}
	return err
}

// AddMod adds a mod to the defaults (server "") or one server, or changes its
// version, prerelease choice and arguments when it is listed already.
func (a *App) AddMod(ctx context.Context, server string, s mods.Spec) error {
	return a.editMods(ctx, server, func(list *[]mods.Spec, _ **bool, _ func(string) bool) error {
		i := slices.IndexFunc(*list, func(m mods.Spec) bool { return m.ID == s.ID })
		if i < 0 {
			*list = append(*list, s)
			return nil
		}
		m := &(*list)[i]
		m.Enabled = nil
		if s.Version != "" {
			m.Version = s.Version
		}
		m.Prerelease = m.Prerelease || s.Prerelease
		if len(s.Args) > 0 {
			args := map[string]string{}
			for k, v := range m.Args {
				args[k] = v
			}
			for k, v := range s.Args {
				args[k] = v
			}
			m.Args = args
		}
		return nil
	})
}

// RemoveMod takes a mod off the defaults, or off one server: a mod of the
// defaults stays listed there with enabled: false.
func (a *App) RemoveMod(ctx context.Context, server, id string) error {
	return a.editMods(ctx, server, func(list *[]mods.Spec, _ **bool, inDefaults func(string) bool) error {
		i := slices.IndexFunc(*list, func(m mods.Spec) bool { return m.ID == id })
		switch {
		case server != "" && inDefaults(id):
			off := false
			if i < 0 {
				*list = append(*list, mods.Spec{ID: id, Enabled: &off})
			} else {
				(*list)[i] = mods.Spec{ID: id, Enabled: &off}
			}
		case i >= 0:
			*list = slices.Delete(*list, i, i+1)
		default:
			return fmt.Errorf("%s isn't in the list", id)
		}
		return nil
	})
}

// PinMod pins a mod to a version or range ("" follows the newest again).
func (a *App) PinMod(ctx context.Context, server, id, version string) error {
	return a.editMods(ctx, server, func(list *[]mods.Spec, _ **bool, inDefaults func(string) bool) error {
		if i := slices.IndexFunc(*list, func(m mods.Spec) bool { return m.ID == id }); i >= 0 {
			(*list)[i].Version = version
			return nil
		}
		if server == "" || !inDefaults(id) {
			return fmt.Errorf("%s isn't in the list: add it first", id)
		}
		*list = append(*list, mods.Spec{ID: id, Version: version})
		return nil
	})
}

// SetLocalPlugins turns a server's own DLLs (mods/local in its volume) on or
// off; on the defaults it is every server's default.
func (a *App) SetLocalPlugins(ctx context.Context, server string, on bool) error {
	return a.editMods(ctx, server, func(_ *[]mods.Spec, local **bool, _ func(string) bool) error {
		*local = &on
		return nil
	})
}

// SetDevCodes stores the mod dev codes in secrets.env.
func (a *App) SetDevCodes(codes []string) error {
	for _, c := range codes {
		if mods.NormalizeCode(c) == "" {
			return fmt.Errorf("%q isn't a dev code", c)
		}
	}
	a.Secrets.SetDevCodes(codes)
	return a.Save()
}
