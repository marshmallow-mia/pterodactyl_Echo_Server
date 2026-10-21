package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/egg"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// SetupOptions connect evr-ptero to a panel and prepare it for Echo VR.
type SetupOptions struct {
	URL        string
	AppKey     string // empty with a local Pterodactyl panel: created automatically
	ClientKey  string // admin account key (power, console; Pterodactyl 2 egg/mount API)
	NodeID     int    // 0 picks this host's node (or the only node)
	BindIP     string // node address for allocations; empty uses existing allocations
	Alias      string // shown in the panel, usually the public IP
	Ports      int    // allocations to make sure exist from 6792
	GameSource string
	DataDir    string
	Image      string
	OwnerID    int
	SkipFiles  bool
	Account    struct{ DiscordID, Password, HTTPKey string }
}

// WingsConfig is the part of Wings' config.yml evr-ptero reads.
type WingsConfig struct {
	UUID   string `yaml:"uuid"`
	Remote string `yaml:"remote"`
	System struct {
		Data string `yaml:"data"`
	} `yaml:"system"`
	AllowedMounts []string `yaml:"allowed_mounts"`
}

// ReadWingsConfig reads the local Wings config (Pterodactyl or Pelican).
func ReadWingsConfig() (WingsConfig, string, error) {
	var wc WingsConfig
	for _, p := range []string{"/etc/pterodactyl/config.yml", "/etc/pelican/config.yml"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		return wc, p, yaml.Unmarshal(b, &wc)
	}
	return wc, "", os.ErrNotExist
}

// Setup prepares the panel: API keys, node, shared files, egg, mounts and
// allocations. Every step checks what already exists, so it can be re-run.
func Setup(ctx context.Context, dir string, o SetupOptions, say func(step, detail string)) (*App, error) {
	if say == nil {
		say = func(string, string) {}
	}
	cfg, err := pcfg.Load(dir)
	if errors.Is(err, pcfg.ErrNotInitialized) {
		cfg, err = pcfg.New(), nil
	}
	if err != nil {
		return nil, err
	}
	sec, err := pcfg.LoadSecrets(dir)
	if err != nil {
		return nil, err
	}
	a := &App{Dir: dir, Cfg: cfg, Secrets: sec}
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&cfg.DataDir, o.DataDir)
	set(&cfg.Image, o.Image)
	set(&cfg.GameSource, o.GameSource)
	set(&cfg.Panel.BindIP, o.BindIP)
	set(&cfg.Panel.Alias, o.Alias)
	if o.AppKey != "" {
		sec[pcfg.SecretAppKey] = o.AppKey
	}
	if o.ClientKey != "" {
		sec[pcfg.SecretClientKey] = o.ClientKey
	}
	if o.Account.DiscordID != "" {
		sec[config.EnvDiscordID], sec[config.EnvPassword], sec[config.EnvHTTPKey] = o.Account.DiscordID, o.Account.Password, o.Account.HTTPKey
	}

	// Panel URL and keys.
	local, isLocal := panel.DetectLocal()
	if o.URL == "" && cfg.Panel.URL == "" {
		// Wings' remote is the address that demonstrably works from this node;
		// APP_URL can point at a port only reachable from outside (NAT).
		if wc, _, err := ReadWingsConfig(); err == nil && wc.Remote != "" {
			o.URL = wc.Remote
		} else if isLocal {
			if url, err := localPanelURL(local.Dir); err == nil {
				o.URL = url
			}
		}
	}
	set(&cfg.Panel.URL, strings.TrimRight(o.URL, "/"))
	if cfg.Panel.URL == "" {
		return nil, errors.New("panel URL is required")
	}
	cfg.Panel.Local = isLocal
	if isLocal {
		a.local = local
		say("Panel", fmt.Sprintf("found %s in %s on this host", local.Kind, local.Dir))
		if sec[pcfg.SecretAppKey] == "" || sec[pcfg.SecretClientKey] == "" {
			admin, err := local.AdminUserID(ctx)
			if err != nil {
				return nil, err
			}
			if sec[pcfg.SecretAppKey] == "" {
				if sec[pcfg.SecretAppKey], err = local.CreateAPIKey(ctx, admin, true, "evr-ptero"); err != nil {
					return nil, fmt.Errorf("creating an application API key: %w", err)
				}
				say("Panel", "created an application API key (memo: evr-ptero)")
			}
			if sec[pcfg.SecretClientKey] == "" {
				if sec[pcfg.SecretClientKey], err = local.CreateAPIKey(ctx, admin, false, "evr-ptero"); err != nil {
					return nil, fmt.Errorf("creating an account API key: %w", err)
				}
				say("Panel", "created an account API key for the admin user (memo: evr-ptero)")
			}
			if err := sec.Save(dir); err != nil { // keep the keys even if a later step fails
				return nil, err
			}
		}
	}
	if sec[pcfg.SecretAppKey] == "" {
		return nil, errors.New("an application API key is required (Admin → Application API)")
	}
	p := a.Panel()
	kind, err := p.Detect(ctx)
	if err != nil {
		return nil, err
	}
	cfg.Panel.Kind, p.Kind = kind, kind
	if local != nil && local.Kind != kind {
		local.Kind = kind
	}
	say("Panel", fmt.Sprintf("%s at %s", kind, cfg.Panel.URL))

	// Node.
	nodes, err := p.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	wc, _, wcErr := ReadWingsConfig()
	if wcErr == nil && wc.System.Data != "" {
		cfg.Panel.WingsData = wc.System.Data
	}
	switch {
	case o.NodeID != 0:
		cfg.Panel.NodeID = o.NodeID
	case cfg.Panel.NodeID != 0:
	default:
		for _, n := range nodes {
			if wcErr == nil && n.UUID == wc.UUID {
				cfg.Panel.NodeID = n.ID
			}
		}
		if cfg.Panel.NodeID == 0 && len(nodes) == 1 {
			cfg.Panel.NodeID = nodes[0].ID
		}
	}
	var node panel.Node
	for _, n := range nodes {
		if n.ID == cfg.Panel.NodeID {
			node = n
		}
	}
	if node.ID == 0 {
		var names []string
		for _, n := range nodes {
			names = append(names, fmt.Sprintf("%d (%s)", n.ID, n.Name))
		}
		return nil, fmt.Errorf("pick a node with --node: %s", strings.Join(names, ", "))
	}
	say("Node", fmt.Sprintf("%s (#%d, %s)", node.Name, node.ID, node.FQDN))

	// Owner of new servers.
	if o.OwnerID != 0 {
		cfg.Panel.OwnerID = o.OwnerID
	}
	if cfg.Panel.OwnerID == 0 {
		users, err := p.Users(ctx)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u.RootAdmin {
				cfg.Panel.OwnerID = u.ID
				break
			}
		}
	}

	// Shared files.
	if cfg.GameSource == "mount" && !o.SkipFiles {
		if err := InstallFiles(ctx, cfg.DataDir, "", "", say); err != nil {
			return a, err
		}
	}

	// Egg.
	if err := a.syncEgg(ctx, say); err != nil {
		return a, err
	}

	// Mounts.
	if cfg.GameSource == "mount" {
		if err := a.syncMounts(ctx, node, say); err != nil {
			return a, err
		}
	}

	// Allocations.
	if o.Ports > 0 && cfg.Panel.BindIP != "" {
		if err := a.ensureAllocations(ctx, o.Ports, say); err != nil {
			return a, err
		}
	}
	if err := a.Save(); err != nil {
		return a, err
	}
	say("Done", "panel ready for Echo VR servers")
	return a, nil
}

func localPanelURL(dir string) (string, error) {
	b, err := os.ReadFile(dir + "/.env")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "APP_URL="); ok {
			return strings.Trim(v, `"' `), nil
		}
	}
	return "", errors.New("no APP_URL")
}

// syncEgg imports the egg, or updates it in place.
func (a *App) syncEgg(ctx context.Context, say func(string, string)) error {
	p := a.Panel()
	format := "ptdl"
	if a.Cfg.Panel.Kind == panel.Pelican {
		format = "plcn"
	}
	data, err := egg.Render(format, a.EggSpec())
	if err != nil {
		return err
	}
	if l := a.Local(); l != nil && a.Cfg.Panel.Kind == panel.Pterodactyl1 {
		id, err := l.ImportEgg(ctx, a.Cfg.Panel.Nest, egg.Name, data)
		if err != nil {
			return fmt.Errorf("importing the egg: %w", err)
		}
		a.Cfg.Panel.EggID = id
		say("Egg", fmt.Sprintf("%q is egg #%d in nest %q", egg.Name, id, a.Cfg.Panel.Nest))
		return nil
	}
	existing, found, err := p.FindEgg(ctx, egg.Name)
	if err != nil {
		return err
	}
	var upd *panel.Egg
	if found {
		upd = &existing
	}
	imported, err := p.ImportEgg(ctx, data, upd)
	if err != nil {
		if found {
			a.Cfg.Panel.EggID = existing.ID
			say("Egg", fmt.Sprintf("could not update egg #%d automatically (%v); re-import egg/%s by hand", existing.ID, err, eggFile(format)))
			return nil
		}
		return fmt.Errorf("%w\nImport egg/%s in the panel, then run setup again", err, eggFile(format))
	}
	if imported.ID == 0 && found {
		imported.ID = existing.ID
	}
	if imported.ID == 0 {
		if e, ok, _ := p.FindEgg(ctx, egg.Name); ok {
			imported = e
		}
	}
	a.Cfg.Panel.EggID = imported.ID
	say("Egg", fmt.Sprintf("%q is egg #%d", egg.Name, imported.ID))
	return nil
}

func eggFile(format string) string {
	if format == "plcn" {
		return "egg-echovr-nevr.yaml"
	}
	return "egg-echovr-nevr.json"
}

// syncMounts creates the two read-only mounts for the shared files.
func (a *App) syncMounts(ctx context.Context, node panel.Node, say func(string, string)) error {
	mounts := []struct{ name, src, dst string }{
		{pcfg.MountGame, a.Cfg.GameDir(), "/opt/echovr/game"},
		{pcfg.MountNevr, a.Cfg.NevrDir(), "/opt/echovr/nevr"},
	}
	var ids []int
	for _, m := range mounts {
		var id int
		if l := a.Local(); l != nil && a.Cfg.Panel.Kind == panel.Pterodactyl1 {
			var err error
			if id, err = l.EnsureMount(ctx, m.name, m.src, m.dst, []int{a.Cfg.Panel.EggID}, []int{node.ID}); err != nil {
				return fmt.Errorf("mount %s: %w", m.name, err)
			}
		} else {
			mt, err := a.Panel().EnsureMount(ctx, m.name, m.src, m.dst, []int{a.Cfg.Panel.EggID}, []int{node.ID})
			if err != nil {
				return fmt.Errorf("mount %s: %w\nCreate it in the panel (source %s, target %s, read-only), attach it to the egg and node, then run setup again", m.name, err, m.src, m.dst)
			}
			id = mt.ID
		}
		ids = append(ids, id)
		say("Mount", fmt.Sprintf("%s: %s → %s (read-only)", m.name, m.src, m.dst))
	}
	a.Cfg.Panel.MountIDs = ids
	paths := []string{a.Cfg.GameDir(), a.Cfg.NevrDir()}
	changed, file, err := EnsureAllowedMounts(paths)
	switch {
	case err != nil && file == "":
		say("Mount", "Wings is not on this host: add "+strings.Join(paths, " and ")+" to allowed_mounts in its config.yml and restart it")
	case err != nil:
		return fmt.Errorf("updating %s: %w", file, err)
	case changed:
		say("Mount", "added the shared paths to allowed_mounts in "+file+" and restarted Wings (running servers keep running)")
	default:
		say("Mount", "Wings already allows the shared paths")
	}
	return nil
}

func (a *App) ensureAllocations(ctx context.Context, n int, say func(string, string)) error {
	p := a.Panel()
	allocs, err := p.Allocations(ctx, a.Cfg.Panel.NodeID)
	if err != nil {
		return err
	}
	have := map[int]bool{}
	for _, al := range allocs {
		if al.IP == a.Cfg.Panel.BindIP {
			have[al.Port] = true
		}
	}
	var missing []string
	for port := pcfg.FirstPort; port < pcfg.FirstPort+n; port++ {
		if !have[port] {
			missing = append(missing, strconv.Itoa(port))
		}
	}
	if len(missing) == 0 {
		say("Allocations", fmt.Sprintf("%s:%d-%d exist", a.Cfg.Panel.BindIP, pcfg.FirstPort, pcfg.FirstPort+n-1))
		return nil
	}
	if err := p.CreateAllocations(ctx, a.Cfg.Panel.NodeID, a.Cfg.Panel.BindIP, a.Cfg.Panel.Alias, missing); err != nil {
		return err
	}
	say("Allocations", fmt.Sprintf("added %d port(s) on %s", len(missing), a.Cfg.Panel.BindIP))
	return nil
}
