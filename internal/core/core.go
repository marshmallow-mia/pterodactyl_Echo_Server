// Package core is evr-ptero's service layer, used by the CLI and the TUI.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/game"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/auth"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/status"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/egg"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// Version of evr-ptero, set by main.
var Version = "dev"

// App bundles configuration and clients.
type App struct {
	Dir     string
	Cfg     *pcfg.Config
	Secrets pcfg.Secrets
	local   *panel.Local
}

// Open loads the configuration.
func Open(dir string) (*App, error) {
	cfg, err := pcfg.Load(dir)
	if err != nil {
		return nil, err
	}
	sec, err := pcfg.LoadSecrets(dir)
	if err != nil {
		return nil, err
	}
	a := &App{Dir: dir, Cfg: cfg, Secrets: sec}
	if cfg.Panel.Local {
		a.local, _ = panel.DetectLocal()
	}
	return a, nil
}

// Save writes config and secrets.
func (a *App) Save() error {
	if err := a.Cfg.Save(a.Dir); err != nil {
		return err
	}
	return a.Secrets.Save(a.Dir)
}

// Panel returns an API client.
func (a *App) Panel() *panel.Client {
	return &panel.Client{URL: a.Cfg.Panel.URL, APIURL: a.Cfg.Panel.APIURL, AppKey: a.Secrets[pcfg.SecretAppKey], ClientKey: a.Secrets[pcfg.SecretClientKey], Kind: a.Cfg.Panel.Kind}
}

// Local returns the on-host panel helper, if the panel runs here.
func (a *App) Local() *panel.Local { return a.local }

// Store is the node's shared runtime store.
func (a *App) Store() nevr.Store { return nevr.Store{Dir: a.Cfg.NevrDir()} }

// Auth is a backend client for the configured account.
func (a *App) Auth() *auth.Client {
	uri := a.Cfg.Defaults.HTTPURI
	if uri == "" {
		uri = config.DefaultHTTPURI
	}
	return &auth.Client{HTTPURI: uri, HTTPKey: a.Secrets[config.EnvHTTPKey]}
}

// Login checks the EchoVRCE account.
func (a *App) Login(ctx context.Context) (auth.Session, error) {
	if m := a.Secrets.MissingAccount(a.DeviceLinked()); len(m) > 0 {
		return auth.Session{}, fmt.Errorf("account not configured: %v missing (run: evr-ptero account set)", m)
	}
	if a.Secrets[config.EnvPassword] == "" {
		return a.refreshDevice(ctx)
	}
	return a.Auth().Login(ctx, a.Secrets[config.EnvDiscordID], a.Secrets[config.EnvPassword])
}

func (a *App) devicePath() string { return filepath.Join(a.Dir, pcfg.DeviceFile) }

// DeviceLinked reports whether `account link` stored a device link.
func (a *App) DeviceLinked() bool {
	_, err := os.Stat(a.devicePath())
	return err == nil
}

// LinkDevice runs the echovrce.com/device flow and stores the refresh token.
func (a *App) LinkDevice(ctx context.Context, show func(url, code string, expires time.Time)) (auth.Session, error) {
	c := a.Auth()
	code, err := c.RequestDevice(ctx)
	if err != nil {
		return auth.Session{}, err
	}
	show(code.URL(), code.Code, code.Expires)
	wctx, cancel := context.WithDeadline(ctx, code.Expires)
	defer cancel()
	linked, err := c.WaitDevice(wctx, code, 0)
	if err != nil {
		return auth.Session{}, err
	}
	if err := auth.WriteCredentials(a.devicePath(), linked); err != nil {
		return auth.Session{}, err
	}
	return linked.Session, nil
}

// refreshDevice signs in with the stored device link and saves the rotated token.
func (a *App) refreshDevice(ctx context.Context) (auth.Session, error) {
	b, err := os.ReadFile(a.devicePath())
	if err != nil {
		return auth.Session{}, fmt.Errorf("no password and no device link (run: evr-ptero account link): %w", err)
	}
	var cred struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(b, &cred); err != nil || cred.RefreshToken == "" {
		return auth.Session{}, fmt.Errorf("%s is not a device link", a.devicePath())
	}
	linked, err := a.Auth().Refresh(ctx, cred.RefreshToken)
	if err != nil {
		return auth.Session{}, err
	}
	if err := auth.WriteCredentials(a.devicePath(), linked); err != nil {
		return auth.Session{}, err
	}
	return linked.Session, nil
}

// installDeviceLink copies the device link into a server's volume, where the
// runtime looks for it (_local/.credentials.json). Needs Wings on this host.
func (a *App) installDeviceLink(s panel.Server) error {
	if !a.DeviceLinked() {
		return nil
	}
	vol := filepath.Join(a.Cfg.Panel.WingsData, s.UUID)
	fi, err := os.Stat(vol)
	if err != nil {
		return fmt.Errorf("server volume %s not found (Wings must run on this host to copy the device link): %w", vol, err)
	}
	b, err := os.ReadFile(a.devicePath())
	if err != nil {
		return err
	}
	dst := filepath.Join(vol, "root", "_local", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		for _, p := range []string{filepath.Join(vol, "root"), filepath.Join(vol, "root", "_local"), dst} {
			_ = os.Chown(p, int(st.Uid), int(st.Gid))
		}
	}
	return nil
}

// InstallFiles puts the game and the runtime into dir (dir/game, dir/nevr).
// The node uses it for the shared mount; the egg's install script uses it for
// servers with GAME_SOURCE=download. runtime is a tag or "current" (newest).
func InstallFiles(ctx context.Context, dir, runtime, mirror string, progress func(step, detail string)) error {
	say := func(s, d string) {
		if progress != nil {
			progress(s, d)
		}
	}
	games := game.Installer{Dir: filepath.Join(dir, "game"), Progress: func(p game.Progress) {
		d := p.Detail
		if p.Percent >= 0 {
			d = fmt.Sprintf("%d%% %s", p.Percent, p.Detail)
		}
		say("Game: "+p.Step, d)
	}}
	if info, err := games.Current(); err == nil {
		say("Game", "build "+info.Build.Version+" already installed")
	} else if _, err := games.FromMirror(ctx, mirror); err != nil {
		return err
	}
	store := nevr.Store{Dir: filepath.Join(dir, "nevr")}
	c := &nevr.Client{}
	var rel nevr.Release
	var err error
	if runtime == "" || runtime == "current" {
		if cur := store.Current(); cur != "" {
			say("Runtime", "nevr-runtime "+cur+" already installed")
			return chmodShared(dir)
		}
		rel, err = c.Newest(ctx, true)
	} else {
		rel, err = c.Find(ctx, runtime)
	}
	if err != nil {
		return err
	}
	say("Runtime", "installing nevr-runtime "+rel.Tag)
	if _, err := store.Install(ctx, rel); err != nil {
		return err
	}
	if store.Current() == "" || runtime == "" || runtime == "current" {
		if err := store.Use(rel.Tag); err != nil {
			return err
		}
	}
	return chmodShared(dir)
}

// chmodShared makes the shared tree readable by any container user.
func chmodShared(dir string) error {
	return filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.Type()&os.ModeSymlink != 0 {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		mode := fi.Mode().Perm() | 0o044
		if d.IsDir() {
			mode |= 0o011
		}
		if mode != fi.Mode().Perm() {
			return os.Chmod(p, mode)
		}
		return nil
	})
}

// Server combines a panel server with what its supervisor reports.
type Server struct {
	Panel  panel.Server
	Port   int
	IP     string
	Status *status.Status
	Power  string // from the client API: running, offline, starting, stopping ("" if unknown)
}

// Name of the server in the panel.
func (s Server) Name() string { return s.Panel.Name }

// State is a one-word summary.
func (s Server) State() string {
	switch {
	case s.Panel.Suspended:
		return "suspended"
	case s.Power == "offline":
		return "stopped"
	case s.Power == "starting" && (s.Status == nil || s.Status.State == status.StateStopped):
		return "starting"
	case s.Status == nil:
		if s.Power == "" {
			return "unknown"
		}
		return s.Power
	case s.Status.Stale(time.Now(), 15*time.Second):
		if s.Power == "running" || s.Power == "" {
			return "starting"
		}
		return s.Power
	}
	return string(s.Status.State)
}

// Servers lists the panel servers that use the Echo VR egg.
func (a *App) Servers(ctx context.Context) ([]Server, error) {
	p := a.Panel()
	all, err := p.Servers(ctx)
	if err != nil {
		return nil, err
	}
	allocs := map[int]panel.Allocation{}
	if a.Cfg.Panel.NodeID != 0 {
		if list, err := p.Allocations(ctx, a.Cfg.Panel.NodeID); err == nil {
			for _, al := range list {
				allocs[al.ID] = al
			}
		}
	}
	var out []Server
	for _, s := range all {
		if s.Egg != a.Cfg.Panel.EggID {
			continue
		}
		sv := Server{Panel: s}
		if al, ok := allocs[s.Allocation]; ok {
			sv.Port, sv.IP = al.Port, al.IP
		}
		if st, err := status.Read(filepath.Join(a.Cfg.Panel.WingsData, s.UUID, status.FileName)); err == nil {
			sv.Status = &st
		}
		if a.Secrets[pcfg.SecretClientKey] != "" {
			rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if r, err := p.Resources(rctx, s.Identifier); err == nil {
				sv.Power = r.State
			}
			cancel()
		}
		out = append(out, sv)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Port < out[j].Port || out[i].Port == out[j].Port && out[i].Name() < out[j].Name()
	})
	return out, nil
}

// Find returns a server by name, identifier, UUID or numeric ID.
func (a *App) Find(ctx context.Context, ref string) (Server, error) {
	all, err := a.Servers(ctx)
	if err != nil {
		return Server{}, err
	}
	for _, s := range all {
		if s.Panel.Name == ref || s.Panel.Identifier == ref || s.Panel.UUID == ref || strconv.Itoa(s.Panel.ID) == ref {
			return s, nil
		}
	}
	return Server{}, fmt.Errorf("no Echo VR server %q in the panel", ref)
}

// Env builds a server's environment from settings and secrets.
func (a *App) Env(set instance.Settings, gameSource string) map[string]string {
	env := map[string]string{
		config.EnvDiscordID:   a.Secrets[config.EnvDiscordID],
		config.EnvPassword:    a.Secrets[config.EnvPassword],
		config.EnvHTTPKey:     a.Secrets[config.EnvHTTPKey],
		"NEVR_REGIONS":        strings.Join(set.Regions, ","),
		"NEVR_GUILDS":         strings.Join(set.Guilds, ","),
		"NEVR_TICK_RATE":      strconv.Itoa(orInt(set.TickRate, config.DefaultTickRate)),
		"NEVR_EMPTY_TTL":      strconv.Itoa(set.EmptyTTL),
		"NEVR_VERSION":        orStr(set.Runtime, "current"),
		"NEVR_PUBLISHER_LOCK": orStr(set.PublisherLock, "echovrce"),
		"GAME_SOURCE":         orStr(gameSource, "mount"),
		"EVR_LOG":             "default",
	}
	if set.LogAll {
		env["EVR_LOG"] = "all"
	}
	return env
}

// SettingsFromEnv reads a server's settings back from its environment.
func SettingsFromEnv(env map[string]string) instance.Settings {
	split := func(s string) []string {
		var out []string
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	tick, _ := strconv.Atoi(env["NEVR_TICK_RATE"])
	ttl, _ := strconv.Atoi(env["NEVR_EMPTY_TTL"])
	return instance.Settings{Regions: split(env["NEVR_REGIONS"]), Guilds: split(env["NEVR_GUILDS"]), TickRate: tick, EmptyTTL: ttl,
		Runtime: env["NEVR_VERSION"], PublisherLock: env["NEVR_PUBLISHER_LOCK"], LogAll: env["EVR_LOG"] == "all"}
}

func orInt(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}

func orStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// CreateServers creates n Echo VR servers on free allocations of the node.
func (a *App) CreateServers(ctx context.Context, n int, set instance.Settings, say func(string)) ([]panel.Server, error) {
	if a.Cfg.Panel.EggID == 0 || a.Cfg.Panel.NodeID == 0 {
		return nil, errors.New("the panel is not set up yet (run: evr-ptero panel setup)")
	}
	if m := a.Secrets.MissingAccount(a.DeviceLinked()); len(m) > 0 {
		return nil, fmt.Errorf("account not configured: %v missing", m)
	}
	set = a.Cfg.Defaults.Merge(set)
	if err := set.Validate(); err != nil {
		return nil, err
	}
	p := a.Panel()
	existing, err := p.Servers(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, s := range existing {
		names[s.Name] = true
	}
	var created []panel.Server
	for i := 0; i < n; i++ {
		alloc, err := a.freeAllocation(ctx)
		if err != nil {
			return created, err
		}
		k := 1
		for names[fmt.Sprintf("Echo VR %d", k)] {
			k++
		}
		name := fmt.Sprintf("Echo VR %d", k)
		names[name] = true
		disk := a.Cfg.Resources.DiskMB
		if a.Cfg.GameSource == "download" && disk < 8192 {
			disk = 8192
		}
		s, err := p.CreateServer(ctx, panel.ServerSpec{
			Name: name, Description: "Echo VR dedicated server (nevr-runtime), managed by evr-ptero",
			User: a.Cfg.Panel.OwnerID, Egg: a.Cfg.Panel.EggID, Image: a.Cfg.Image, Startup: "evr-supervisor",
			Env: a.Env(set, a.Cfg.GameSource), Allocation: alloc.ID,
			MemoryMB: a.Cfg.Resources.MemoryMB, DiskMB: disk, CPU: a.Cfg.Resources.CPU,
			StartAfterInstall: false,
		})
		if err != nil {
			return created, fmt.Errorf("creating %s: %w", name, err)
		}
		if say != nil {
			say(fmt.Sprintf("created %s on port %d", name, alloc.Port))
		}
		created = append(created, s)
	}
	// Wait for the install step, then start.
	for _, s := range created {
		if err := a.startWhenInstalled(ctx, s, say); err != nil {
			return created, err
		}
	}
	return created, nil
}

func (a *App) attachMounts(ctx context.Context, s panel.Server) error {
	if len(a.Cfg.Panel.MountIDs) == 0 {
		return errors.New("no shared-file mounts are set up (run: evr-ptero panel setup)")
	}
	if l := a.Local(); l != nil && l.Kind == panel.Pterodactyl1 {
		return l.AttachMount(ctx, s.ID, a.Cfg.Panel.MountIDs)
	}
	return a.Panel().AttachServerMounts(ctx, s.ID, a.Cfg.Panel.MountIDs)
}

func (a *App) startWhenInstalled(ctx context.Context, s panel.Server, say func(string)) error {
	// Pterodactyl 2 only attaches mounts to installed servers, so wait for
	// the install step to finish first (harmless on the other panels).
	if a.Cfg.GameSource == "mount" {
		deadline := time.Now().Add(30 * time.Minute)
		for {
			err := a.attachMounts(ctx, s)
			if err == nil {
				break
			}
			var ae *panel.APIError
			if !errors.As(err, &ae) || (ae.Status != 409 && ae.Status != 400 && ae.Status != 422) || time.Now().After(deadline) {
				return fmt.Errorf("%s: attaching the shared files: %w", s.Name, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
		if say != nil {
			say(s.Name + ": shared game files attached")
		}
	}
	if a.DeviceLinked() {
		if err := a.installDeviceLink(s); err != nil {
			return err
		}
		if say != nil {
			say(s.Name + ": device link installed")
		}
	}
	if a.Secrets[pcfg.SecretClientKey] == "" {
		if say != nil {
			say(s.Name + ": start it in the panel (no account API key configured)")
		}
		return nil
	}
	p := a.Panel()
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		err := p.Power(ctx, s.Identifier, "start")
		if err == nil {
			if say != nil {
				say(s.Name + ": starting")
			}
			return nil
		}
		// 409 while the server is still installing; 502/504 while Wings is
		// busy, e.g. pulling the image on the first start.
		var ae *panel.APIError
		if !errors.As(err, &ae) || (ae.Status != 409 && ae.Status != 502 && ae.Status != 504) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("%s did not finish installing", s.Name)
}

// freeAllocation returns an unassigned allocation on the node, creating the
// next port if none is free.
func (a *App) freeAllocation(ctx context.Context) (panel.Allocation, error) {
	p := a.Panel()
	allocs, err := p.Allocations(ctx, a.Cfg.Panel.NodeID)
	if err != nil {
		return panel.Allocation{}, err
	}
	used := map[int]bool{}
	sort.Slice(allocs, func(i, j int) bool { return allocs[i].Port < allocs[j].Port })
	for _, al := range allocs {
		if a.Cfg.Panel.BindIP != "" && al.IP != a.Cfg.Panel.BindIP {
			continue
		}
		used[al.Port] = true
		if !al.Assigned && al.Port >= pcfg.FirstPort && al.Port < pcfg.FirstPort+200 && a.portFree(al.IP, al.Port) {
			return al, nil
		}
	}
	if a.Cfg.Panel.BindIP == "" {
		return panel.Allocation{}, errors.New("no free allocation and no bind IP configured to create one")
	}
	port := pcfg.FirstPort
	for used[port] || !a.portFree(a.Cfg.Panel.BindIP, port) {
		port++
	}
	if err := p.CreateAllocations(ctx, a.Cfg.Panel.NodeID, a.Cfg.Panel.BindIP, a.Cfg.Panel.Alias, []string{strconv.Itoa(port)}); err != nil {
		return panel.Allocation{}, err
	}
	allocs, err = p.Allocations(ctx, a.Cfg.Panel.NodeID)
	if err != nil {
		return panel.Allocation{}, err
	}
	for _, al := range allocs {
		if al.Port == port && al.IP == a.Cfg.Panel.BindIP && !al.Assigned {
			return al, nil
		}
	}
	return panel.Allocation{}, fmt.Errorf("allocation %s:%d was not created", a.Cfg.Panel.BindIP, port)
}

// portFree reports whether ip:port/udp is unused on this host. Only checked
// when the panel's node is this host; otherwise assume it is free.
func (a *App) portFree(ip string, port int) bool {
	if !a.Cfg.Panel.Local {
		return true
	}
	c, err := net.ListenPacket("udp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// UpdateServer applies settings (merged over the server's current ones) and,
// if it is running, restarts it.
func (a *App) UpdateServer(ctx context.Context, s Server, change func(*instance.Settings), restart bool) error {
	env := s.Panel.Env()
	set := SettingsFromEnv(env)
	change(&set)
	if err := set.Validate(); err != nil {
		return err
	}
	newEnv := a.Env(set, orStr(env["GAME_SOURCE"], a.Cfg.GameSource))
	p := a.Panel()
	if err := p.UpdateStartup(ctx, s.Panel, a.Cfg.Panel.EggID, orStr(s.Panel.Container.Image, a.Cfg.Image), "evr-supervisor", newEnv, true); err != nil {
		return err
	}
	if restart && a.Secrets[pcfg.SecretClientKey] != "" && s.Power == "running" {
		return p.Power(ctx, s.Panel.Identifier, "restart")
	}
	return nil
}

// Power sends a power signal.
func (a *App) Power(ctx context.Context, s Server, signal string) error {
	return a.Panel().Power(ctx, s.Panel.Identifier, signal)
}

// Delete removes a server from the panel.
func (a *App) Delete(ctx context.Context, s Server) error {
	return a.Panel().DeleteServer(ctx, s.Panel.ID, false)
}

// Console streams a server's console.
func (a *App) Console(ctx context.Context, s Server, line func(string)) error {
	return a.Panel().Console(ctx, s.Panel.Identifier, line)
}

// EggSpec returns the egg for the configured image.
func (a *App) EggSpec() egg.Spec { return egg.Spec{Image: a.Cfg.Image, Version: Version} }
