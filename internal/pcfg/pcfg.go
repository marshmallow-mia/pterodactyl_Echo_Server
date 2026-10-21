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
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
)

// Defaults.
const (
	DefaultDir     = "/etc/evr-ptero"
	DefaultDataDir = "/srv/echovr"
	DefaultImage   = "ghcr.io/marshmallow-mia/echovr-pterodactyl:2"
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
	DataDir string `yaml:"data_dir"` // shared game files + runtimes on the node
	Image   string `yaml:"image"`
	// GameSource for new servers: mount (shared files) or download (private copy).
	GameSource string            `yaml:"game_source"`
	Defaults   instance.Settings `yaml:"defaults"`
	Resources  Resources         `yaml:"resources"`
	Runtime    RuntimePolicy     `yaml:"runtime"`
	Notify     struct {
		Events []string `yaml:"events,omitempty"`
	} `yaml:"notify,omitempty"`
	Metrics struct {
		Listen string `yaml:"listen,omitempty"`
	} `yaml:"metrics,omitempty"`

	path string
}

// Panel describes the panel and what evr-ptero created in it.
type Panel struct {
	URL      string     `yaml:"url"`
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
}

// Resources for new servers (0 = unlimited).
type Resources struct {
	MemoryMB int `yaml:"memory_mb"`
	DiskMB   int `yaml:"disk_mb"`
	CPU      int `yaml:"cpu_percent"`
}

// RuntimePolicy is the nevr-runtime update policy.
type RuntimePolicy struct {
	Channel    string `yaml:"channel"`
	Pinned     string `yaml:"pinned,omitempty"`
	AutoUpdate bool   `yaml:"auto_update"`
	CheckHours int    `yaml:"check_hours,omitempty"`
}

// New returns defaults.
func New() *Config {
	c := &Config{Version: 1, DataDir: DefaultDataDir, Image: DefaultImage, GameSource: "mount",
		Defaults:  instance.Settings{TickRate: config.DefaultTickRate, PublisherLock: "echovrce", Runtime: "current"},
		Resources: Resources{MemoryMB: 0, DiskMB: 2048},
		Runtime:   RuntimePolicy{Channel: "pre", AutoUpdate: true, CheckHours: 6},
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
var ErrNotInitialized = errors.New("evr-ptero is not set up yet (run: evr-ptero init)")

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
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
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
	switch c.Runtime.Channel {
	case "pre", "stable":
	case "pinned":
		if c.Runtime.Pinned == "" {
			errs = append(errs, errors.New("runtime.channel is pinned but runtime.pinned is empty"))
		}
	default:
		errs = append(errs, fmt.Errorf("runtime.channel must be pre, stable or pinned, got %q", c.Runtime.Channel))
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
	b, err := yaml.Marshal(c)
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

// MissingAccount lists missing EchoVRCE account secrets.
func (s Secrets) MissingAccount() []string {
	var out []string
	for _, k := range []string{config.EnvDiscordID, config.EnvPassword, config.EnvHTTPKey} {
		if s[k] == "" {
			out = append(out, k)
		}
	}
	return out
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
