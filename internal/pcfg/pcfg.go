// Package pcfg is evr-ptero's configuration: config.yaml for settings and
// secrets.env (0600) for API keys and the EchoVRCE account.
package pcfg

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/updates"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/version"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
)

// ImageRepo is where the server image is published.
const ImageRepo = "ghcr.io/marshmallow-mia/echovr-pterodactyl"

// DefaultImage is the server image released with this build (see SetVersion).
// It is not written to config.yaml, so updating evr-ptero and running setup
// again moves the egg and the servers to the matching image; set image there
// to pin another one.
var DefaultImage = ImageRepo + ":edge"

// SetVersion selects the image released with tool version v.
func SetVersion(v string) { DefaultImage = ImageRepo + ":" + version.ImageTag(v) }

// Defaults.
const (
	DefaultDir     = "/etc/evr-ptero"
	DefaultDataDir = "/srv/echovr"
	DefaultNest    = "Echo VR"
	FirstPort      = 6792
)

// Secret names.
const (
	SecretAppKey    = "PANEL_APP_KEY"
	SecretClientKey = "PANEL_CLIENT_KEY"
	SecretWebhook   = "EVR_DISCORD_WEBHOOK"
)

// Mount names created in the panel.
const (
	MountGame = "echovr-game"
	MountNevr = "echovr-nevr"
)

// Config is evr-ptero's configuration.
type Config struct {
	Version int    `yaml:"version"`
	Panel   Panel  `yaml:"panel"`
	DataDir string `yaml:"data_dir"`        // shared game files + runtimes on the node
	Image   string `yaml:"image,omitempty"` // empty = DefaultImage
	// GameSource for new servers: mount (shared files) or download (private copy).
	GameSource string            `yaml:"game_source"`
	Defaults   instance.Settings `yaml:"defaults"`
	Resources  Resources         `yaml:"resources"`
	Runtime    RuntimePolicy     `yaml:"runtime"`
	Updates    UpdatePolicy      `yaml:"updates"`
	Notify     struct {
		Events []string `yaml:"events,omitempty"`
	} `yaml:"notify,omitempty"`
	Metrics struct {
		Listen string `yaml:"listen,omitempty"`
	} `yaml:"metrics,omitempty"`
	// HiddenLegacy lists old-egg servers (by UUID) the dashboard no longer points at.
	HiddenLegacy []string `yaml:"hidden_legacy,omitempty"`

	path string
}

// Panel describes the panel and what evr-ptero created in it.
type Panel struct {
	URL string `yaml:"url"`
	// APIURL, when set, is used for API calls from this host (e.g. the panel's
	// local address when a proxy with a login sits in front of URL).
	APIURL   string     `yaml:"api_url,omitempty"`
	Kind     panel.Kind `yaml:"kind"`
	Local    bool       `yaml:"local"` // panel runs on this host (enables Pterodactyl 1.x extras)
	NodeID   int        `yaml:"node_id"`
	OwnerID  int        `yaml:"owner_user_id"` // user new servers belong to
	EggID    int        `yaml:"egg_id"`
	Nest     string     `yaml:"nest,omitempty"`
	MountIDs []int      `yaml:"mount_ids,omitempty"`
	// BindIP is the node address allocations are created on; Alias is shown in
	// the panel (usually the public IP).
	BindIP string `yaml:"bind_ip"`
	Alias  string `yaml:"alias,omitempty"`
	// WingsData is where Wings keeps server volumes (for status files).
	WingsData string `yaml:"wings_data,omitempty"`
	// NoPlugin: the Echo VR match plugin was removed on purpose; setup leaves
	// the panel without it.
	NoPlugin bool `yaml:"no_plugin,omitempty"`
}

// Resources for new servers (0 = unlimited).
type Resources struct {
	MemoryMB int `yaml:"memory_mb"`
	DiskMB   int `yaml:"disk_mb"`
	CPU      int `yaml:"cpu_percent"`
}

// RuntimePolicy is the nevr-runtime update policy.
type RuntimePolicy struct {
	// Updater is UpdaterNevr (from v6.18.0 on the runtime updates itself to
	// its channel's release; evr-ptero keeps the node's store, and so the
	// host DLL, on that release) or UpdaterHost (evr-ptero updates it, with
	// the canary, and turns the runtime's own update off).
	Updater string `yaml:"updater"`
	// Channel is one of nEVR's channels (stable, beta, ...: the release nEVR's
	// manifest names) or "pinned" (Pinned; only with UpdaterHost). Before
	// updater existed it was "pre" (GitHub's newest), "stable" or "pinned".
	Channel string `yaml:"channel"`
	Pinned  string `yaml:"pinned,omitempty"`
	// Before updates: existed; read once and moved there.
	AutoUpdate *bool `yaml:"auto_update,omitempty"`
	CheckHours int   `yaml:"check_hours,omitempty"`
}

// Runtime updaters (RuntimePolicy.Updater).
const (
	UpdaterNevr = "nevr"
	UpdaterHost = "evr-ptero"
)

// SelfUpdateEnv is the servers' environment for the policy: who keeps a
// runtime current (evr-supervisor's EVR_RUNTIME_UPDATER) and the channel.
func (r RuntimePolicy) SelfUpdateEnv() map[string]string {
	who := nevr.UpdaterHost
	if r.Updater == UpdaterNevr {
		who = nevr.UpdaterNevr
	}
	ch := nevr.ChannelName(r.Channel)
	if ch == "pinned" { // a pin is evr-ptero's; the channel is not used then
		ch = nevr.DefaultChannel
	}
	return map[string]string{"EVR_RUNTIME_UPDATER": who, "NEVR_UPDATE_CHANNEL": ch}
}

// UpdatePolicy says how each part is updated: off, notify or auto.
type UpdatePolicy struct {
	CheckHours int    `yaml:"check_hours,omitempty"` // default 6
	Window     string `yaml:"window,omitempty"`      // "04:00-06:00": automatic installs only then
	Self       string `yaml:"self"`                  // evr-ptero itself
	Game       string `yaml:"game"`
	Runtime    string `yaml:"runtime"`
	Panel      string `yaml:"panel"` // the panel on this host
	Wings      string `yaml:"wings"` // Wings on this host
	// PanelChannel: "" = the installed one; stable or beta (Pterodactyl).
	PanelChannel string `yaml:"panel_channel,omitempty"`
}

// Settings converts the policy for the updates package.
func (u UpdatePolicy) Settings() updates.Settings {
	return updates.Settings{CheckHours: u.CheckHours, Window: u.Window, Modes: map[string]updates.Mode{
		"self": updates.Mode(u.Self), "game": updates.Mode(u.Game), "runtime": updates.Mode(u.Runtime),
		"panel": updates.Mode(u.Panel), "wings": updates.Mode(u.Wings)}}
}

func (u *UpdatePolicy) modes() map[string]*string {
	return map[string]*string{"self": &u.Self, "game": &u.Game, "runtime": &u.Runtime, "panel": &u.Panel, "wings": &u.Wings}
}

// migrate moves settings from older config versions and fills defaults.
func (c *Config) migrate() {
	// A config from before updater: evr-ptero kept updating, on nEVR's
	// stable channel from now on (the old channels followed GitHub's
	// releases, which no nEVR channel does). A pin stays.
	if c.Runtime.Updater == "" {
		c.Runtime.Updater = UpdaterHost
		if c.Runtime.Channel != "pinned" {
			c.Runtime.Channel = nevr.DefaultChannel
		}
	}
	c.Runtime.Channel = nevr.ChannelName(c.Runtime.Channel)
	if c.Runtime.AutoUpdate != nil {
		if c.Updates.Runtime == "" {
			c.Updates.Runtime = string(updates.Notify)
			if *c.Runtime.AutoUpdate {
				c.Updates.Runtime = string(updates.Auto)
			}
		}
		c.Runtime.AutoUpdate = nil
	}
	if c.Runtime.CheckHours > 0 {
		if c.Updates.CheckHours == 0 {
			c.Updates.CheckHours = c.Runtime.CheckHours
		}
		c.Runtime.CheckHours = 0
	}
	def := New().Updates
	defs := def.modes()
	for k, v := range c.Updates.modes() {
		if *v == "" {
			*v = *defs[k]
		}
	}
}

// New returns defaults.
func New() *Config {
	c := &Config{Version: 1, DataDir: DefaultDataDir, Image: DefaultImage, GameSource: "mount",
		Defaults:  instance.Settings{TickRate: config.DefaultTickRate, PublisherLock: "echovrce", Runtime: "current"},
		Resources: Resources{MemoryMB: 0, DiskMB: 2048},
		Runtime:   RuntimePolicy{Updater: UpdaterNevr, Channel: nevr.DefaultChannel},
		Updates:   UpdatePolicy{CheckHours: 6, Self: "notify", Game: "notify", Runtime: "auto", Panel: "notify", Wings: "notify"},
	}
	c.Panel.Nest = DefaultNest
	c.Panel.WingsData = "/var/lib/pterodactyl/volumes"
	c.Metrics.Listen = "127.0.0.1:9470"
	return c
}

// Dir returns $EVR_PTERO_CONFIG_DIR or /etc/evr-ptero.
func Dir() string {
	if d := os.Getenv("EVR_PTERO_CONFIG_DIR"); d != "" {
		return d
	}
	return DefaultDir
}

// ErrNotInitialized means there is no config yet.
var ErrNotInitialized = errors.New("evr-ptero is not set up yet")

// Load reads dir/config.yaml.
func Load(dir string) (*Config, error) {
	p := filepath.Join(dir, "config.yaml")
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, ErrNotInitialized
	}
	if err != nil {
		return nil, err
	}
	c := New()
	c.Updates = UpdatePolicy{}
	c.Runtime = RuntimePolicy{} // what the file leaves out, migrate fills in
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	c.migrate()
	if c.Image == "" {
		c.Image = DefaultImage
	}
	c.path = p
	return c, c.Validate()
}

// Path of the loaded file.
func (c *Config) Path() string { return c.path }

// Validate checks the config.
func (c *Config) Validate() error {
	var errs []error
	if !filepath.IsAbs(c.DataDir) {
		errs = append(errs, fmt.Errorf("data_dir must be absolute, got %q", c.DataDir))
	}
	if c.GameSource != "mount" && c.GameSource != "download" {
		errs = append(errs, fmt.Errorf("game_source must be mount or download, got %q", c.GameSource))
	}
	switch u := c.Runtime.Updater; {
	case u != UpdaterNevr && u != UpdaterHost:
		errs = append(errs, fmt.Errorf("runtime.updater must be %s or %s, got %q", UpdaterNevr, UpdaterHost, u))
	case c.Runtime.Channel == "pinned" && u == UpdaterNevr:
		errs = append(errs, fmt.Errorf("runtime.channel pinned needs runtime.updater %s (nEVR would update itself away from the pin)", UpdaterHost))
	}
	switch ch := c.Runtime.Channel; {
	case ch == "pinned":
		if c.Runtime.Pinned == "" {
			errs = append(errs, errors.New("runtime.channel is pinned but runtime.pinned is empty"))
		}
	case !nevr.ChannelOK(ch):
		errs = append(errs, fmt.Errorf("runtime.channel must be one of nEVR's channels (stable, beta, ...) or pinned, got %q", ch))
	}
	for k, m := range c.Updates.modes() {
		if _, err := updates.ParseMode(*m, updates.Notify); err != nil {
			errs = append(errs, fmt.Errorf("updates.%s: %w", k, err))
		}
	}
	if err := updates.ValidWindow(c.Updates.Window); err != nil {
		errs = append(errs, fmt.Errorf("updates.window: %w", err))
	}
	switch c.Updates.PanelChannel {
	case "", "stable", "beta":
	default:
		errs = append(errs, fmt.Errorf("updates.panel_channel must be stable or beta, got %q", c.Updates.PanelChannel))
	}
	if err := c.Defaults.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("defaults: %w", err))
	}
	return errors.Join(errs...)
}

// Save writes config.yaml.
func (c *Config) Save(dir string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out := *c
	if out.Image == DefaultImage {
		out.Image = ""
	}
	b, err := yaml.Marshal(&out)
	if err != nil {
		return err
	}
	c.path = filepath.Join(dir, "config.yaml")
	return writeAtomic(c.path, append([]byte("# evr-ptero configuration\n"), b...), 0o644)
}

// GameDir and NevrDir are the shared directories on the node.
func (c *Config) GameDir() string { return filepath.Join(c.DataDir, "game") }
func (c *Config) NevrDir() string { return filepath.Join(c.DataDir, "nevr") }

// Secrets from secrets.env.
type Secrets map[string]string

// LoadSecrets reads dir/secrets.env (missing = empty).
func LoadSecrets(dir string) (Secrets, error) {
	s := Secrets{}
	f, err := os.Open(filepath.Join(dir, "secrets.env"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			s[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return s, sc.Err()
}

// Save writes secrets.env with mode 0600.
func (s Secrets) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# evr-ptero secrets (mode 0600): panel API keys, EchoVRCE host account, webhook.\n")
	for _, k := range keys {
		if strings.ContainsAny(s[k], "\r\n") {
			return fmt.Errorf("secret %s contains a line break", k)
		}
		fmt.Fprintf(&b, "%s=%s\n", k, s[k])
	}
	return writeAtomic(filepath.Join(dir, "secrets.env"), []byte(b.String()), 0o600)
}

// DeviceFile holds the echovrce.com device link (refresh token), 0600.
const DeviceFile = "device.json"

// Missing lists the account secrets a password sign-in still lacks: the
// Discord ID. Without a password nothing is missing: from nevr-runtime v6.9.0
// on each server signs in by a code it prints on its console (a 5.x server by
// a device link made earlier).
func (s Secrets) Missing() []string {
	if s[config.EnvPassword] != "" && s[config.EnvDiscordID] == "" {
		return []string{config.EnvDiscordID}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
