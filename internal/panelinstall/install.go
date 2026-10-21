package panelinstall

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

// Product versions the installer is tested with.
var Versions = map[string]struct{ Panel, Wings, WingsRepo, PanelRepo string }{
	"pterodactyl-stable": {"v1.15.1", "v1.13.4", "pterodactyl/wings", "pterodactyl/panel"},
	"pterodactyl-beta":   {"v2.0.0-beta.2", "v1.13.4", "pterodactyl/wings", "pterodactyl/panel"},
	"pelican":            {"v1.0.0-rc.1", "v1.0.0-rc.1", "pelican/wings", "pelican/panel"},
}

// Options describe the panel to install.
type Options struct {
	Product string // pterodactyl or pelican
	Channel string // stable or beta (Pterodactyl)
	FQDN    string
	Email   string // admin account (stays in the panel)
	// LEEmail registers the Let's Encrypt account; empty registers without one.
	LEEmail string
	// Admin login; the password is generated when empty.
	AdminUser, AdminPassword string
	Timezone                 string
	// TLS: letsencrypt, proxy, existing (CertFile/KeyFile) or none (plain http).
	// proxy: a reverse proxy (e.g. Pangolin/Traefik) terminates HTTPS for the
	// panel and forwards to HTTPPort; the host still gets a Let's Encrypt
	// certificate through that route for Wings, which browsers reach directly.
	TLS               string
	CertFile, KeyFile string
	// Ports nginx listens on inside this host. Behind a NAT that maps 80→81
	// and 443→444, use HTTPPort 81 and HTTPSPorts 444,443 (443 lets the host
	// reach itself by its public name).
	HTTPPort   int
	HTTPSPorts []int
	WingsPort  int
	SFTPPort   int
	NodeName   string
	// WingsFQDN is the name browsers reach Wings by. With TLS proxy it is a
	// second proxy hostname that forwards plain HTTP to port 443 here.
	WingsFQDN string
	// Database for Pelican: mariadb (default) or sqlite.
	Database string
	// Override the pinned versions.
	PanelVersion, WingsVersion string
}

// Result is what the installer created.
type Result struct {
	Product, Kind, URL, Dir string
	AdminUser, AdminEmail   string
	AdminPassword           string `json:"-"`
	NodeID                  int
	// APIURL is where tools on this host reach the panel (bypassing a proxy).
	APIURL            string
	AppKey, ClientKey string `json:"-"`
	WingsConfig       string
}

// key returns the Versions key.
func (o Options) key() string {
	if o.Product == "pelican" {
		return "pelican"
	}
	if o.Channel == "beta" {
		return "pterodactyl-beta"
	}
	return "pterodactyl-stable"
}

func (o *Options) defaults() error {
	if o.Product == "" {
		o.Product = "pterodactyl"
	}
	if o.Product != "pterodactyl" && o.Product != "pelican" {
		return fmt.Errorf("product must be pterodactyl or pelican, got %q", o.Product)
	}
	if o.Channel == "" {
		o.Channel = "stable"
	}
	if o.Channel != "stable" && o.Channel != "beta" {
		return fmt.Errorf("channel must be stable or beta, got %q", o.Channel)
	}
	if o.FQDN == "" || net.ParseIP(o.FQDN) != nil && o.TLS != "none" {
		return errors.New("a domain name for the panel is required (TLS needs a name, not an IP)")
	}
	if o.Email == "" || !strings.Contains(o.Email, "@") {
		return errors.New("an admin email address is required")
	}
	if o.AdminUser == "" {
		o.AdminUser = "admin"
	}
	if o.AdminPassword == "" {
		o.AdminPassword = randomString(20)
	}
	if o.Timezone == "" {
		o.Timezone = "UTC"
	}
	if o.TLS == "" {
		o.TLS = "letsencrypt"
	}
	switch o.TLS {
	case "letsencrypt", "none", "proxy":
	case "existing":
		if o.CertFile == "" || o.KeyFile == "" {
			return errors.New("--tls existing needs --cert and --key")
		}
	default:
		return fmt.Errorf("tls must be letsencrypt, proxy, existing or none, got %q", o.TLS)
	}
	if o.HTTPPort == 0 {
		o.HTTPPort = 80
	}
	if len(o.HTTPSPorts) == 0 {
		o.HTTPSPorts = []int{443}
	}
	if o.WingsPort == 0 {
		o.WingsPort = 8080
	}
	if o.SFTPPort == 0 {
		o.SFTPPort = 2022
	}
	if o.TLS == "proxy" {
		if o.WingsFQDN == "" || o.WingsFQDN == o.FQDN {
			return errors.New("--tls proxy needs --wings-fqdn: a second proxy hostname that forwards http to port 443 of this host")
		}
		o.WingsPort = 443 // browsers reach Wings on 443 through the proxy; Pterodactyl uses one port for both
		o.HTTPSPorts = nil
	}
	if o.WingsFQDN == "" {
		o.WingsFQDN = o.FQDN
	}
	if o.NodeName == "" {
		h, _ := os.Hostname()
		o.NodeName = regexp.MustCompile(`[^\w .-]`).ReplaceAllString(h, "-")
		if o.TLS == "proxy" {
			if o.WingsFQDN == "" || o.WingsFQDN == o.FQDN {
				return errors.New("--tls proxy needs --wings-fqdn: a second proxy hostname that forwards http to port 443 of this host")
			}
			o.WingsPort = 443 // browsers reach Wings on 443 through the proxy; Pterodactyl uses one port for both
			o.HTTPSPorts = nil
		}
		if o.WingsFQDN == "" {
			o.WingsFQDN = o.FQDN
		}
		if o.NodeName == "" {
			o.NodeName = "node1"
		}
	}
	if o.Database == "" {
		o.Database = "mariadb"
	}
	if o.Product != "pelican" && o.Database != "mariadb" {
		return errors.New("Pterodactyl needs MariaDB")
	}
	v := Versions[o.key()]
	if o.PanelVersion == "" {
		o.PanelVersion = v.Panel
	}
	if o.WingsVersion == "" {
		o.WingsVersion = v.Wings
	}
	return nil
}

const stateFile = "/etc/evr-ptero/install.env"

// state is kept between runs so passwords and keys are reused.
type state map[string]string

func loadState() state {
	s := state{}
	f, err := os.Open(stateFile)
	if err != nil {
		return s
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok && !strings.HasPrefix(k, "#") {
			s[k] = v
		}
	}
	return s
}

func (s state) save() error {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Written by evr-ptero panel install (mode 0600). Keep it: re-runs reuse these values.\n")
	for _, k := range []string{"PRODUCT", "PANEL_DIR", "URL", "ADMIN_EMAIL", "ADMIN_USER", "ADMIN_PASSWORD", "DB_PASSWORD", "NODE_ID"} {
		if v, ok := s[k]; ok {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	return os.WriteFile(stateFile, []byte(b.String()), 0o600)
}

func randomString(n int) string {
	const chars = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	for i := range b {
		x, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[x.Int64()]
	}
	return string(b)
}

// installer holds what the steps share.
type installer struct {
	o      Options
	st     state
	dir    string // panel directory
	owner  string // www-data
	php    string // php version, e.g. 8.3
	osName string // bookworm / trixie
	arch   string
	res    Result
}

// Install runs the full installation. With r.DryRun only the script is built.
func Install(ctx context.Context, o Options, r *Runner) (Result, error) {
	if err := o.defaults(); err != nil {
		return Result{}, err
	}
	in := &installer{o: o, st: loadState(), owner: "www-data", arch: runtime.GOARCH}
	in.osName = osCodename()
	if in.st["DB_PASSWORD"] == "" {
		in.st["DB_PASSWORD"] = randomString(24)
	}
	if prev := in.st["ADMIN_PASSWORD"]; prev != "" && in.st["ADMIN_EMAIL"] == o.Email {
		in.o.AdminPassword = prev // the account already exists with this password
	}
	in.st["ADMIN_PASSWORD"], in.st["ADMIN_EMAIL"], in.st["ADMIN_USER"] = in.o.AdminPassword, o.Email, o.AdminUser
	in.st["PRODUCT"] = o.key()
	if o.Product == "pelican" {
		in.dir, in.php = "/var/www/pelican", "8.4"
		in.res.Kind = "pelican"
	} else {
		in.dir, in.php = "/var/www/pterodactyl", "8.3"
		in.res.Kind = map[string]string{"stable": "pterodactyl1", "beta": "pterodactyl2"}[o.Channel]
	}
	scheme := "https"
	if o.TLS == "none" {
		scheme = "http"
	}
	in.res.URL = scheme + "://" + o.FQDN
	if o.TLS == "none" && o.HTTPPort != 80 {
		in.res.URL += ":" + strconv.Itoa(o.HTTPPort)
	}
	if o.TLS == "proxy" {
		in.res.APIURL = "http://127.0.0.1"
		if o.HTTPPort != 80 {
			in.res.APIURL += ":" + strconv.Itoa(o.HTTPPort)
		}
	}
	in.st["PANEL_DIR"], in.st["URL"] = in.dir, in.res.URL
	in.res.Product, in.res.Dir = o.key(), in.dir
	in.res.AdminUser, in.res.AdminEmail, in.res.AdminPassword = o.AdminUser, o.Email, in.o.AdminPassword
	if !r.DryRun {
		if err := in.st.save(); err != nil {
			return in.res, err
		}
	}
	err := r.Execute(ctx, in.steps())
	if !r.DryRun {
		_ = in.st.save()
	}
	return in.res, err
}

func osCodename() string {
	b, _ := os.ReadFile("/etc/os-release")
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "VERSION_CODENAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

func (in *installer) artisan(args string) string {
	return fmt.Sprintf("cd %s && runuser -u %s -- env HOME=/tmp php artisan %s -n", in.dir, in.owner, args)
}

func (in *installer) steps() []Step {
	o := in.o
	pelican := o.Product == "pelican"
	beta := !pelican && o.Channel == "beta"
	return []Step{
		{Name: "Check the host", Run: func(ctx context.Context, r *Runner) error {
			if r.DryRun {
				return nil
			}
			if os.Geteuid() != 0 {
				return errors.New("run as root")
			}
			if in.osName != "bookworm" && in.osName != "trixie" {
				return fmt.Errorf("Debian 12 (bookworm) or 13 (trixie) is required, this is %q", in.osName)
			}
			if in.arch != "amd64" && in.arch != "arm64" {
				return fmt.Errorf("unsupported architecture %s", in.arch)
			}
			other := "/var/www/pelican/artisan"
			if pelican {
				other = "/var/www/pterodactyl/artisan"
			}
			if Exists(other) {
				return fmt.Errorf("another panel is installed (%s); remove it first with: evr-ptero panel uninstall", filepath.Dir(other))
			}
			for _, p := range append([]int{o.HTTPPort, o.WingsPort, o.SFTPPort}, o.HTTPSPorts...) {
				if busy(p) && !ownsPort(p) {
					return fmt.Errorf("TCP port %d is in use by another program", p)
				}
			}
			return nil
		}},
		{Name: "Base packages", Done: func(context.Context) bool {
			return Has("curl") && Has("cron") && Has("unzip") && Has("git") && Has("rsync")
		}, Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `apt-get update -qq && apt-get install -y -qq ca-certificates curl gnupg tar unzip git cron rsync lsb-release >/dev/null`)
		}},
		{Name: "PHP repository (deb.sury.org)", Done: func(context.Context) bool {
			return Exists("/etc/apt/sources.list.d/php.list") || (pelican && in.osName == "trixie")
		}, Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `curl -sSLo /tmp/debsuryorg-archive-keyring.deb https://packages.sury.org/debsuryorg-archive-keyring.deb
dpkg -i /tmp/debsuryorg-archive-keyring.deb >/dev/null
. /etc/os-release
echo "deb [signed-by=/usr/share/keyrings/debsuryorg-archive-keyring.gpg] https://packages.sury.org/php/ $VERSION_CODENAME main" > /etc/apt/sources.list.d/php.list
apt-get update -qq`)
		}},
		{Name: "PHP " + in.php + ", database, Redis and nginx", Done: func(context.Context) bool {
			return Has("php"+in.php) && Has("nginx") && (o.Database == "sqlite" || Has("mariadb")) && Has("redis-server")
		}, Run: func(ctx context.Context, r *Runner) error {
			exts := "common,cli,gd,mysql,mbstring,bcmath,xml,fpm,curl,zip"
			if beta || pelican {
				exts += ",intl"
			}
			if pelican {
				exts += ",sqlite3"
			}
			pkgs := fmt.Sprintf("php%s php%s-{%s} nginx redis-server", in.php, in.php, exts)
			if o.Database != "sqlite" {
				pkgs += " mariadb-server"
			}
			if o.TLS == "letsencrypt" {
				pkgs += " certbot"
			}
			return r.Sh(ctx, fmt.Sprintf(`apt-get install -y -qq %s >/dev/null
update-alternatives --set php /usr/bin/php%s
systemctl enable --now php%s-fpm redis-server >/dev/null 2>&1`, pkgs, in.php, in.php))
		}},
		{Name: "Composer", Done: func(context.Context) bool { return Exists("/usr/local/bin/composer") }, Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `curl -sS https://getcomposer.org/installer | php -- --quiet --install-dir=/usr/local/bin --filename=composer`)
		}},
		{Name: "Database", Done: func(context.Context) bool { return o.Database == "sqlite" }, Run: func(ctx context.Context, r *Runner) error {
			user := "pterodactyl"
			if pelican {
				user = "pelican"
			}
			pw := in.st["DB_PASSWORD"]
			return r.Sh(ctx, fmt.Sprintf(`systemctl enable --now mariadb >/dev/null 2>&1
mariadb -e "CREATE DATABASE IF NOT EXISTS panel; CREATE USER IF NOT EXISTS '%[1]s'@'127.0.0.1' IDENTIFIED BY '%[2]s'; ALTER USER '%[1]s'@'127.0.0.1' IDENTIFIED BY '%[2]s'; GRANT ALL PRIVILEGES ON panel.* TO '%[1]s'@'127.0.0.1' WITH GRANT OPTION; FLUSH PRIVILEGES;"`, user, pw))
		}},
		{Name: "Panel files " + o.PanelVersion, Done: func(context.Context) bool { return Exists(in.dir + "/artisan") }, Run: func(ctx context.Context, r *Runner) error {
			repo := Versions[o.key()].PanelRepo
			script := fmt.Sprintf(`mkdir -p %[1]s && cd %[1]s
curl -fsSL -o /tmp/panel.tar.gz https://github.com/%[2]s/releases/download/%[3]s/panel.tar.gz
`, in.dir, repo, o.PanelVersion)
			if pelican {
				script += fmt.Sprintf(`curl -fsSL -o /tmp/panel.checksum https://github.com/%s/releases/download/%s/checksum.txt
want=$(grep -oE '[0-9a-f]{64}' /tmp/panel.checksum | head -1); got=$(sha256sum /tmp/panel.tar.gz | cut -d' ' -f1)
[ "$want" = "$got" ] || { echo "panel.tar.gz checksum mismatch"; exit 1; }
`, repo, o.PanelVersion)
			}
			script += `tar -xzf /tmp/panel.tar.gz && rm -f /tmp/panel.tar.gz
chmod -R 755 storage/* bootstrap/cache/
[ -f .env ] || cp .env.example .env
chown -R www-data:www-data ` + in.dir
			return r.Sh(ctx, script)
		}},
		{Name: "PHP dependencies (composer install)", Done: func(context.Context) bool { return Exists(in.dir + "/vendor/autoload.php") }, Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, fmt.Sprintf(`cd %s && chown -R www-data:www-data . && runuser -u www-data -- env HOME=/tmp COMPOSER_HOME=/tmp/composer composer install --no-dev --optimize-autoloader --no-interaction --quiet`, in.dir))
		}},
		{Name: "Panel configuration", Run: func(ctx context.Context, r *Runner) error {
			if pelican {
				return in.pelicanEnv(ctx, r)
			}
			telemetry := "0" // 1.15.1 treats any non-empty string except "0"-ish as true
			if beta {
				telemetry = "false"
			}
			if err := r.Sh(ctx, fmt.Sprintf(`cd %[1]s
grep -q '^APP_KEY=base64:' .env || %[2]s
%[3]s
%[4]s`, in.dir, in.artisan("key:generate --force"),
				in.artisan(fmt.Sprintf("p:environment:setup --author=%s --url=%s --timezone=%s --cache=redis --session=redis --queue=redis --redis-host=127.0.0.1 --redis-port=6379 --settings-ui=true --telemetry=%s --redis-pass=", shq(o.Email), shq(in.res.URL), shq(o.Timezone), telemetry)),
				in.artisan(fmt.Sprintf("p:environment:database --host=127.0.0.1 --port=3306 --database=panel --username=pterodactyl --password=%s", in.st["DB_PASSWORD"])))); err != nil {
				return err
			}
			if o.TLS == "proxy" && !r.DryRun {
				// The proxy talks plain http; trust its X-Forwarded-* headers.
				if err := setEnvFile(in.dir+"/.env", map[string]string{"TRUSTED_PROXIES": "*"}); err != nil {
					return err
				}
				return r.Sh(ctx, "chown www-data:www-data "+in.dir+"/.env && "+in.artisan("config:clear"))
			}
			return nil
		}},
		{Name: "Database tables", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, in.artisan("migrate --seed --force"))
		}},
		{Name: "Admin user " + o.Email, Done: func(ctx context.Context) bool { return in.userExists(ctx) }, Run: func(ctx context.Context, r *Runner) error {
			args := fmt.Sprintf("p:user:make --email=%s --username=%s --password=%s --admin=1", shq(o.Email), shq(o.AdminUser), shq(in.o.AdminPassword))
			if !pelican {
				args += " --name-first=Echo --name-last=Admin"
			}
			return r.Sh(ctx, in.artisan(args))
		}},
		{Name: "Queue worker and scheduler", Run: func(ctx context.Context, r *Runner) error {
			if pelican {
				return r.Sh(ctx, fmt.Sprintf(`cd %[1]s && HOME=/tmp php artisan p:environment:queue-service -n --overwrite
grep -q 'artisan schedule:run' <(crontab -u www-data -l 2>/dev/null) || (crontab -u www-data -l 2>/dev/null; echo '* * * * * php %[1]s/artisan schedule:run >> /dev/null 2>&1') | crontab -u www-data -`, in.dir))
			}
			queues := "high,standard,low"
			if beta {
				queues += ",default"
			}
			unit := fmt.Sprintf(`[Unit]
Description=Pterodactyl Queue Worker
After=redis-server.service

[Service]
User=www-data
Group=www-data
Restart=always
ExecStart=/usr/bin/php %s/artisan queue:work --queue=%s --sleep=3 --tries=3
StartLimitInterval=180
StartLimitBurst=30
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, in.dir, queues)
			if err := r.WriteFile("/etc/systemd/system/pteroq.service", unit, 0o644); err != nil {
				return err
			}
			if err := r.WriteFile("/etc/cron.d/pterodactyl", fmt.Sprintf("* * * * * www-data php %s/artisan schedule:run >> /dev/null 2>&1\n", in.dir), 0o644); err != nil {
				return err
			}
			return r.Sh(ctx, `systemctl daemon-reload && systemctl enable --now pteroq >/dev/null 2>&1 && systemctl restart pteroq`)
		}},
		{Name: "Web server and TLS", Run: func(ctx context.Context, r *Runner) error { return in.webServer(ctx, r) }},
		{Name: "Docker", Done: func(context.Context) bool { return Has("docker") && Active("docker") }, Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `curl -sSL https://get.docker.com/ | CHANNEL=stable sh >/dev/null && systemctl enable --now docker >/dev/null 2>&1 && docker info >/dev/null`)
		}},
		{Name: "Wings " + o.WingsVersion, Run: func(ctx context.Context, r *Runner) error { return in.wings(ctx, r) }},
		{Name: "Node and API keys", Run: func(ctx context.Context, r *Runner) error { return in.node(ctx, r) }},
	}
}

// pelicanEnv configures Pelican without its web installer (and without
// p:environment:database, which always asks for confirmation).
func (in *installer) pelicanEnv(ctx context.Context, r *Runner) error {
	o := in.o
	env := map[string]string{"APP_INSTALLED": "true", "APP_TIMEZONE": o.Timezone}
	if o.TLS != "none" {
		env["SESSION_SECURE_COOKIE"] = "true"
	}
	if o.TLS == "proxy" {
		env["TRUSTED_PROXIES"] = "*"
	}
	if o.Database == "sqlite" {
		env["DB_CONNECTION"], env["DB_DATABASE"] = "sqlite", "database.sqlite"
	} else {
		for k, v := range map[string]string{"DB_CONNECTION": "mariadb", "DB_HOST": "127.0.0.1", "DB_PORT": "3306", "DB_DATABASE": "panel", "DB_USERNAME": "pelican", "DB_PASSWORD": in.st["DB_PASSWORD"]} {
			env[k] = v
		}
	}
	for k, v := range map[string]string{"CACHE_STORE": "redis", "SESSION_DRIVER": "redis", "QUEUE_CONNECTION": "redis", "REDIS_HOST": "127.0.0.1", "REDIS_PORT": "6379"} {
		env[k] = v
	}
	if err := r.Sh(ctx, in.artisan("p:environment:setup --url="+shq(in.res.URL))); err != nil {
		return err
	}
	if r.DryRun {
		var lines []string
		for k, v := range env {
			lines = append(lines, k+"="+v)
		}
		r.Script.WriteString("# set in " + in.dir + "/.env:\n# " + strings.Join(lines, "\n# ") + "\n")
		return nil
	}
	if err := setEnvFile(in.dir+"/.env", env); err != nil {
		return err
	}
	return r.Sh(ctx, fmt.Sprintf("chown www-data:www-data %s/.env && %s", in.dir, in.artisan("config:clear")))
}

func setEnvFile(path string, kv map[string]string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	seen := map[string]bool{}
	for i, l := range lines {
		if k, _, ok := strings.Cut(l, "="); ok {
			if v, set := kv[strings.TrimSpace(k)]; set {
				lines[i] = k + "=" + v
				seen[k] = true
			}
		}
	}
	for k, v := range kv {
		if !seen[k] {
			lines = append(lines, k+"="+v)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o640)
}

func (in *installer) userExists(ctx context.Context) bool {
	model := `\Pterodactyl\Models\User`
	if in.o.Product == "pelican" {
		model = `App\Models\User`
	}
	out, err := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf(`cd %s && runuser -u www-data -- env HOME=/tmp php artisan tinker --execute='echo "EVR:" . (%s::where("email", %q)->exists() ? 1 : 0);' 2>/dev/null`, in.dir, model, in.o.Email)).Output()
	return err == nil && strings.Contains(string(out), "EVR:1")
}

func (in *installer) webServer(ctx context.Context, r *Runner) error {
	o := in.o
	name := "pterodactyl"
	if o.Product == "pelican" {
		name = "pelican"
	}
	acme := `    location ^~ /.well-known/acme-challenge/ { root /var/www/letsencrypt; default_type "text/plain"; }
`
	redirect := fmt.Sprintf(`server {
    listen %d;
    server_name %s;
%s    location / { return 301 https://$server_name$request_uri; }
}
`, o.HTTPPort, o.FQDN, acme)
	// What answers on the HTTP port: a redirect, or (behind a proxy, or
	// without TLS) the panel itself.
	httpSite := redirect
	if o.TLS == "proxy" || o.TLS == "none" {
		httpSite = in.appServer(fmt.Sprintf("    listen %d;\n", o.HTTPPort), "", "", acme)
	}
	cert, key := o.CertFile, o.KeyFile
	if o.TLS == "letsencrypt" {
		cert, key = "/etc/letsencrypt/live/"+strings.ToLower(o.FQDN)+"/fullchain.pem", "/etc/letsencrypt/live/"+strings.ToLower(o.FQDN)+"/privkey.pem"
	}
	site := "/etc/nginx/sites-available/" + name + ".conf"
	setup := `rm -f /etc/nginx/sites-enabled/default
mkdir -p /var/www/letsencrypt
ln -sf ` + site + ` /etc/nginx/sites-enabled/` + name + `.conf
nginx -t && systemctl enable nginx >/dev/null 2>&1 && systemctl reload-or-restart nginx`
	if o.TLS == "letsencrypt" && (!Exists(cert) || r.DryRun) {
		// Serve only HTTP first, so the ACME challenge can be answered.
		if err := r.WriteFile(site, httpSite, 0o644); err != nil {
			return err
		}
		if err := r.Sh(ctx, setup); err != nil {
			return err
		}
		reg := "--register-unsafely-without-email"
		if o.LEEmail != "" {
			reg = "-m " + shq(o.LEEmail)
		}
		if err := r.Sh(ctx, fmt.Sprintf(`certbot certonly --webroot -w /var/www/letsencrypt -d %s --non-interactive --agree-tos %s --keep-until-expiring`, o.FQDN, reg)); err != nil {
			hint := fmt.Sprintf("Let's Encrypt must reach http://%s/.well-known/acme-challenge/ (port 80 from outside, arriving here on %d)", o.FQDN, o.HTTPPort)
			return fmt.Errorf("%w\n%s", err, hint)
		}
	}
	conf := httpSite
	if o.TLS == "letsencrypt" || o.TLS == "existing" {
		listen := ""
		h2 := nginxSupportsHTTP2On()
		for _, p := range o.HTTPSPorts {
			if h2 {
				listen += fmt.Sprintf("    listen %d ssl;\n", p)
			} else {
				listen += fmt.Sprintf("    listen %d ssl http2;\n", p)
			}
		}
		if h2 {
			listen += "    http2 on;\n"
		}
		conf += "\n" + in.appServer(listen, cert, key, "")
	}
	if err := r.WriteFile(site, conf, 0o644); err != nil {
		return err
	}
	if err := r.Sh(ctx, setup); err != nil {
		return err
	}
	if o.TLS == "letsencrypt" || o.TLS == "existing" {
		// Reach the panel by its name locally, without NAT hairpinning or a
		// proxy login (Wings and evr-ptero talk to it this way).
		if err := r.Sh(ctx, fmt.Sprintf(`grep -qE '^127\.0\.0\.1[[:space:]].*[[:space:]]%[1]s([[:space:]]|$)' /etc/hosts || echo '127.0.0.1 %[1]s # evr-ptero: panel and Wings on this host' >> /etc/hosts`, regexp.QuoteMeta(o.FQDN))); err != nil {
			return err
		}
	}
	if o.TLS == "letsencrypt" {
		hook := "#!/bin/sh\nsystemctl reload nginx\nsystemctl restart wings 2>/dev/null || true\n"
		return r.WriteFile("/etc/letsencrypt/renewal-hooks/deploy/evr-reload.sh", hook, 0o755)
	}
	return nil
}

func nginxSupportsHTTP2On() bool {
	out, err := exec.Command("nginx", "-v").CombinedOutput()
	if err != nil {
		return false
	}
	m := regexp.MustCompile(`nginx/(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		return false
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	pat, _ := strconv.Atoi(m[3])
	return maj > 1 || maj == 1 && (min > 25 || min == 25 && pat >= 1)
}

func (in *installer) appServer(listen, cert, key, extraLocations string) string {
	o := in.o
	name, sock := "pterodactyl", "/run/php/php"+in.php+"-fpm.sock"
	extra := ""
	if o.Product == "pelican" {
		name = "pelican"
	}
	if o.Product == "pterodactyl" && o.Channel == "beta" {
		extra = "    location ^~ /assets/ { location ~ /\\. { deny all; } try_files $uri =404; }\n"
	}
	extra += extraLocations
	tls := ""
	if cert != "" {
		tls = fmt.Sprintf(`    ssl_certificate %s;
    ssl_certificate_key %s;
    ssl_session_cache shared:SSL:10m;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers "ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384";
    ssl_prefer_server_ciphers on;
`, cert, key)
	}
	return fmt.Sprintf(`server {
%[1]s    server_name %[2]s;
    server_tokens off;
    absolute_redirect off;
    root %[3]s/public;
    index index.php;
    access_log /var/log/nginx/%[4]s.app-access.log;
    error_log  /var/log/nginx/%[4]s.app-error.log error;
    client_max_body_size 100m;
    client_body_timeout 120s;
    sendfile off;
%[5]s    add_header X-Content-Type-Options nosniff;
    add_header X-XSS-Protection "1; mode=block";
    add_header X-Robots-Tag none;
    add_header Content-Security-Policy "frame-ancestors 'self'";
    add_header X-Frame-Options DENY;
    add_header Referrer-Policy same-origin;
%[7]s    location / { try_files $uri $uri/ /index.php?$query_string; }
    location ~ \.php$ {
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:%[6]s;
        fastcgi_index index.php;
        include fastcgi_params;
        fastcgi_param PHP_VALUE "upload_max_filesize = 100M \n post_max_size=100M";
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param HTTP_PROXY "";
        fastcgi_intercept_errors off;
        fastcgi_buffer_size 16k;
        fastcgi_buffers 4 16k;
        fastcgi_connect_timeout 300;
        fastcgi_send_timeout 300;
        fastcgi_read_timeout 300;
    }
    location ~ /\.ht { deny all; }
}
`, listen, o.FQDN, in.dir, name, tls, sock, extra)
}

func (in *installer) wingsPaths() (cfgDir, unitName, desc string) {
	if in.o.Product == "pelican" {
		return "/etc/pelican", "wings", "Wings Daemon"
	}
	return "/etc/pterodactyl", "wings", "Pterodactyl Wings Daemon"
}

func (in *installer) wings(ctx context.Context, r *Runner) error {
	o := in.o
	cfgDir, unit, desc := in.wingsPaths()
	repo := Versions[o.key()].WingsRepo
	have := ""
	if out, err := exec.Command("/usr/local/bin/wings", "version").Output(); err == nil {
		have = string(out)
	}
	if !strings.Contains(have, strings.TrimPrefix(o.WingsVersion, "v")) || r.DryRun {
		if err := r.Sh(ctx, fmt.Sprintf(`mkdir -p %s /var/run/wings
curl -fsSL -o /usr/local/bin/wings.new https://github.com/%s/releases/download/%s/wings_linux_%s
chmod 0755 /usr/local/bin/wings.new && mv /usr/local/bin/wings.new /usr/local/bin/wings`, cfgDir, repo, o.WingsVersion, in.arch)); err != nil {
			return err
		}
	}
	return r.WriteFile("/etc/systemd/system/"+unit+".service", fmt.Sprintf(`[Unit]
Description=%s
After=docker.service
Requires=docker.service
PartOf=docker.service

[Service]
User=root
WorkingDirectory=%s
LimitNOFILE=4096
PIDFile=/var/run/wings/daemon.pid
RuntimeDirectory=wings
ExecStart=/usr/local/bin/wings
Restart=on-failure
StartLimitInterval=180
StartLimitBurst=30
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, desc, cfgDir), 0o644)
}

// tinker runs PHP in the panel and returns what follows "EVR:" in the output.
func (in *installer) tinker(ctx context.Context, r *Runner, code string) (string, error) {
	cmd := fmt.Sprintf(`cd %s && runuser -u www-data -- env HOME=/tmp php artisan tinker --execute=%s`, in.dir, shq(code))
	out, err := r.Output(ctx, cmd)
	if r.DryRun {
		return "", nil
	}
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "EVR:"); ok {
			return v, nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("artisan tinker: %v: %s", err, out)
	}
	return "", fmt.Errorf("artisan tinker gave no result: %s", out)
}

func (in *installer) node(ctx context.Context, r *Runner) error {
	o := in.o
	pelican := o.Product == "pelican"
	scheme := "https"
	if o.TLS == "none" {
		scheme = "http"
	}
	memMB, diskMB := hostResources()
	ns := `\Pterodactyl`
	if pelican {
		ns = `App`
	}
	// Node.
	id, err := in.tinker(ctx, r, fmt.Sprintf(`echo "EVR:" . (%s\Models\Node::where("name", %q)->value("id") ?? 0);`, ns, o.NodeName))
	if err != nil {
		return err
	}
	if id == "0" || id == "" && !r.DryRun {
		loc := ""
		if !pelican {
			lid, err := in.tinker(ctx, r, `$l = \Pterodactyl\Models\Location::query()->orderBy("id")->first(); echo "EVR:" . ($l ? $l->id : 0);`)
			if err != nil {
				return err
			}
			if lid == "0" {
				if err := r.Sh(ctx, in.artisan(`p:location:make --short=main --long="Main location"`)); err != nil {
					return err
				}
				if lid, err = in.tinker(ctx, r, `echo "EVR:" . \Pterodactyl\Models\Location::query()->orderBy("id")->value("id");`); err != nil {
					return err
				}
			}
			loc = " --locationId=" + lid
		}
		proxy := 0
		if o.TLS == "proxy" {
			proxy = 1
		}
		args := fmt.Sprintf(`p:node:make --name=%s --description="Echo VR node (evr-ptero)"%s --fqdn=%s --public=1 --scheme=%s --proxy=%d --maintenance=0 --maxMemory=%d --overallocateMemory=0 --maxDisk=%d --overallocateDisk=0 --uploadSize=100 --daemonListeningPort=%d --daemonSFTPPort=%d`,
			shq(o.NodeName), loc, o.WingsFQDN, scheme, proxy, memMB, diskMB, o.WingsPort, o.SFTPPort)
		if pelican {
			args += fmt.Sprintf(" --daemonConnectingPort=%d --daemonBase=/var/lib/pelican/volumes --maxCpu=0 --overallocateCpu=0", o.WingsPort)
		} else {
			args += " --daemonBase=/var/lib/pterodactyl/volumes"
		}
		if err := r.Sh(ctx, in.artisan(args)); err != nil {
			return err
		}
		if id, err = in.tinker(ctx, r, fmt.Sprintf(`echo "EVR:" . (%s\Models\Node::where("name", %q)->value("id") ?? 0);`, ns, o.NodeName)); err != nil {
			return err
		}
	}
	in.res.NodeID, _ = strconv.Atoi(id)
	in.st["NODE_ID"] = id
	// Wings configuration from the panel.
	cfgDir, unit, _ := in.wingsPaths()
	cfg := cfgDir + "/config.yml"
	in.res.WingsConfig = cfg
	if !Exists(cfg) || r.DryRun {
		if err := r.Sh(ctx, fmt.Sprintf(`mkdir -p %s && cd %s && runuser -u www-data -- env HOME=/tmp php artisan p:node:configuration %s --format=yaml > %s.new && chmod 600 %s.new && mv %s.new %s`, cfgDir, in.dir, id, cfg, cfg, cfg, cfg)); err != nil {
			return err
		}
	}
	if !r.DryRun && o.TLS == "existing" {
		if err := patchWingsTLS(cfg, o.CertFile, o.KeyFile); err != nil {
			return err
		}
	}
	if !r.DryRun && o.TLS == "proxy" {
		// Wings reaches the panel locally (no proxy login in the way); the
		// browser's origin is the public panel URL.
		if err := patchWingsRemote(cfg, in.res.APIURL, in.res.URL); err != nil {
			return err
		}
	}
	if err := r.Sh(ctx, fmt.Sprintf(`systemctl daemon-reload && systemctl enable %[1]s >/dev/null 2>&1 && systemctl restart %[1]s`, unit)); err != nil {
		return err
	}
	// API keys for evr-ptero.
	if r.DryRun {
		r.Script.WriteString("# create an application API key and an account API key for the admin in the panel\n")
		return nil
	}
	uid, err := in.tinker(ctx, r, fmt.Sprintf(`echo "EVR:" . %s\Models\User::where("email", %q)->value("id");`, ns, o.Email))
	if err != nil {
		return err
	}
	if in.res.AppKey, err = in.apiKey(ctx, r, uid, true); err != nil {
		return fmt.Errorf("application API key: %w", err)
	}
	if in.res.ClientKey, err = in.apiKey(ctx, r, uid, false); err != nil {
		return fmt.Errorf("account API key: %w", err)
	}
	return nil
}

func (in *installer) apiKey(ctx context.Context, r *Runner, uid string, app bool) (string, error) {
	o := in.o
	typ := "TYPE_ACCOUNT"
	if app {
		typ = "TYPE_APPLICATION"
	}
	var code string
	switch {
	case o.Product == "pelican":
		perms := `[]`
		if app {
			perms = `array_fill_keys(App\Models\ApiKey::getPermissionList(), 3)`
		}
		code = fmt.Sprintf(`$k = app(App\Services\Api\KeyCreationService::class)->setKeyType(App\Models\ApiKey::%s)->handle(["user_id"=>%s,"memo"=>"evr-ptero","allowed_ips"=>[],"permissions"=>%s]); echo "EVR:" . $k->identifier . $k->token;`, typ, uid, perms)
	case o.Channel == "beta":
		perms := `[]`
		if app {
			perms = `["r_servers"=>3,"r_nodes"=>3,"r_allocations"=>3,"r_users"=>3,"r_locations"=>3,"r_eggs"=>3,"r_database_hosts"=>3,"r_server_databases"=>3]`
		}
		code = fmt.Sprintf(`$k = app(\Pterodactyl\Contracts\Api\CreatesApiKeys::class)->create(\Pterodactyl\Models\ApiKey::%s, ["memo"=>"evr-ptero","user_id"=>%s], %s); echo "EVR:" . $k->identifier . decrypt($k->token);`, typ, uid, perms)
	default:
		perms := `[]`
		if app {
			perms = `["r_servers"=>3,"r_nodes"=>3,"r_allocations"=>3,"r_users"=>3,"r_locations"=>3,"r_nests"=>3,"r_eggs"=>3,"r_database_hosts"=>3,"r_server_databases"=>3]`
		}
		code = fmt.Sprintf(`$k = app(\Pterodactyl\Services\Api\KeyCreationService::class)->setKeyType(\Pterodactyl\Models\ApiKey::%s)->handle(["user_id"=>%s,"memo"=>"evr-ptero","allowed_ips"=>[]], %s); echo "EVR:" . $k->identifier . decrypt($k->token);`, typ, uid, perms)
	}
	return in.tinker(ctx, r, code)
}

func patchWingsRemote(cfg, remote, origin string) error {
	b, err := os.ReadFile(cfg)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return err
	}
	m["remote"] = remote
	origins, _ := m["allowed_origins"].([]any)
	has := false
	for _, o := range origins {
		has = has || o == origin
	}
	if !has {
		m["allowed_origins"] = append(origins, origin)
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(cfg, out, 0o600)
}

func patchWingsTLS(cfg, cert, key string) error {
	b, err := os.ReadFile(cfg)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return err
	}
	api, _ := m["api"].(map[string]any)
	if api == nil {
		return errors.New("wings config has no api section")
	}
	ssl, _ := api["ssl"].(map[string]any)
	if ssl == nil {
		ssl = map[string]any{}
		api["ssl"] = ssl
	}
	ssl["enabled"], ssl["cert"], ssl["key"] = true, cert, key
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(cfg, out, 0o600)
}

// hostResources returns RAM and free disk (for the node limits) in MB.
func hostResources() (memMB, diskMB int) {
	memMB, diskMB = 4096, 20480
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if f := strings.Fields(l); len(f) >= 2 && f[0] == "MemTotal:" {
				if kb, err := strconv.Atoi(f[1]); err == nil {
					memMB = kb / 1024
				}
			}
		}
	}
	var fs syscall.Statfs_t
	if syscall.Statfs("/var/lib", &fs) == nil {
		diskMB = int(fs.Bavail * uint64(fs.Bsize) >> 20)
	}
	return
}

func busy(port int) bool {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return true
	}
	l.Close()
	return false
}

// ownsPort reports whether nginx or wings already listen there (a re-run).
func ownsPort(port int) bool {
	out, _ := exec.Command("ss", "-ltnpH", "sport", "= :"+strconv.Itoa(port)).Output()
	s := string(out)
	return strings.Contains(s, "nginx") || strings.Contains(s, "wings")
}

// MarshalResult renders a result for --json output.
func MarshalResult(r Result) ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
