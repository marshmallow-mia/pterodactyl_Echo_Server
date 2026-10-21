package panelinstall

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/ghrel"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"
)

// BackupDir keeps the panel backups taken before upgrades.
const BackupDir = "/var/backups/evr-ptero"

// Installed describes the panel and Wings on this host.
type Installed struct {
	Product string // pterodactyl or pelican
	Channel string // stable or beta (Pterodactyl); stable (Pelican)
	Dir     string // panel directory; empty without a panel here
	Panel   string // panel version, e.g. 2.0.0-beta.2
	Wings   string // Wings version; empty without Wings here
}

// PanelRepo and WingsRepo are the GitHub repos of the product.
func (in Installed) PanelRepo() string { return Versions[in.key()].PanelRepo }
func (in Installed) WingsRepo() string { return Versions[in.key()].WingsRepo }

func (in Installed) key() string {
	if in.Product == "pelican" {
		return "pelican"
	}
	return "pterodactyl-" + in.Channel
}

var appVersion = regexp.MustCompile(`'version'\s*=>\s*'([^']+)'`)

// DetectInstalled finds the panel and Wings on this host.
func DetectInstalled() (Installed, error) {
	var in Installed
	st := loadState()
	in.Dir = st["PANEL_DIR"]
	switch {
	case strings.HasPrefix(st["PRODUCT"], "pelican"):
		in.Product, in.Channel = "pelican", "stable"
	case st["PRODUCT"] == "pterodactyl-beta":
		in.Product, in.Channel = "pterodactyl", "beta"
	case st["PRODUCT"] != "":
		in.Product, in.Channel = "pterodactyl", "stable"
	}
	if in.Dir == "" {
		for _, d := range []string{"/var/www/pelican", "/var/www/pterodactyl"} {
			if Exists(d + "/artisan") {
				in.Dir = d
			}
		}
	}
	if in.Dir != "" {
		b, err := os.ReadFile(filepath.Join(in.Dir, "config", "app.php"))
		if err != nil {
			return in, err
		}
		if m := appVersion.FindSubmatch(b); m != nil {
			in.Panel = strings.TrimPrefix(string(m[1]), "v")
		}
		if in.Product == "" {
			in.Product, in.Channel = "pterodactyl", "stable"
			if strings.Contains(in.Dir, "pelican") {
				in.Product = "pelican"
			} else if v, err := nevr.ParseVersion(in.Panel); err == nil && v.Major >= 2 {
				in.Channel = "beta"
			}
		}
	}
	if out, err := exec.Command("/usr/local/bin/wings", "version").Output(); err == nil {
		if f := strings.Fields(string(out)); len(f) >= 2 {
			in.Wings = strings.TrimLeft(f[1], "v")
		}
		if in.Product == "" {
			in.Product, in.Channel = "pterodactyl", "stable"
			if Exists("/etc/pelican/config.yml") {
				in.Product = "pelican"
			}
		}
	}
	if in.Product == "" {
		return in, errors.New("no panel or Wings on this host")
	}
	return in, nil
}

// InChannel reports whether a panel release belongs to channel ("" = the
// installed one): Pterodactyl stable = full 1.x releases, beta = 2.x and
// later; Pelican has one line.
func (in Installed) InChannel(r ghrel.Release, channel string) bool {
	if channel == "" {
		channel = in.Channel
	}
	if in.Product == "pelican" {
		return true
	}
	if channel == "beta" {
		return r.Version.Major >= 2
	}
	cur, err := nevr.ParseVersion(in.Panel)
	return !r.Prerelease && (err != nil || r.Version.Major == cur.Major)
}

// PanelReleases lists panel releases with a panel.tar.gz that are not older
// than the installed version, newest first.
func PanelReleases(ctx context.Context, c *ghrel.Client, in Installed) ([]ghrel.Release, error) {
	return newerReleases(ctx, c, in.PanelRepo(), in.Panel, func(r ghrel.Release) bool { _, ok := r.Assets["panel.tar.gz"]; return ok })
}

// WingsReleases lists Wings releases for this machine that are not older
// than the installed version, newest first.
func WingsReleases(ctx context.Context, c *ghrel.Client, in Installed) ([]ghrel.Release, error) {
	return newerReleases(ctx, c, in.WingsRepo(), in.Wings, func(r ghrel.Release) bool { _, ok := r.Assets[wingsAsset()]; return ok })
}

func wingsAsset() string { return "wings_linux_" + runtime.GOARCH }

func newerReleases(ctx context.Context, c *ghrel.Client, repo, installed string, keep func(ghrel.Release) bool) ([]ghrel.Release, error) {
	all, err := c.List(ctx, repo)
	if err != nil {
		return nil, err
	}
	cur, curErr := nevr.ParseVersion(installed)
	var out []ghrel.Release
	for _, r := range all {
		if keep(r) && (curErr != nil || !r.Version.Less(cur)) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[j].Version.Less(out[i].Version) })
	return out, nil
}

// PanelBackup is what UpgradePanel saved before changing anything.
type PanelBackup struct {
	Dir, PanelDir string
	SQLite        bool
}

// UpgradePanel installs rel over the panel in in.Dir: backup, maintenance
// mode, new files, dependencies, migrations, queue restart. The caller checks
// the panel afterwards and calls RestorePanel when it does not work.
func UpgradePanel(ctx context.Context, in Installed, rel ghrel.Release, c *ghrel.Client, r *Runner) (PanelBackup, error) {
	b := PanelBackup{PanelDir: in.Dir, Dir: filepath.Join(BackupDir, fmt.Sprintf("panel-%s-%s", in.Panel, time.Now().Format("20060102-150405")))}
	envFile, _ := os.ReadFile(filepath.Join(in.Dir, ".env"))
	b.SQLite = regexp.MustCompile(`(?m)^DB_CONNECTION=sqlite`).Match(envFile)
	db := "panel"
	if m := regexp.MustCompile(`(?m)^DB_DATABASE=("?)([^"\n]+)("?)$`).FindSubmatch(envFile); m != nil && !b.SQLite {
		db = string(m[2])
	}
	queue := "pteroq"
	if in.Product == "pelican" {
		queue = "pelican-queue"
	}
	tarball := filepath.Join(os.TempDir(), "panel-"+rel.Tag+".tar.gz")
	steps := []Step{
		{Name: "Back up the panel to " + b.Dir, Run: func(ctx context.Context, r *Runner) error {
			dump := fmt.Sprintf("mariadb-dump --single-transaction --routines %s > %s/database.sql", shq(db), shq(b.Dir))
			if b.SQLite {
				dump = "# sqlite: the database file is inside the panel directory"
			}
			return r.Sh(ctx, fmt.Sprintf(`mkdir -p %[1]s && chmod 700 %[2]s %[1]s
cp -a %[3]s %[1]s/files
%[4]s
ls -1dt %[2]s/panel-* 2>/dev/null | tail -n +4 | xargs -r rm -rf`, shq(b.Dir), BackupDir, shq(in.Dir), dump))
		}},
		{Name: "Download panel " + rel.Tag, Run: func(ctx context.Context, r *Runner) error {
			return fetchVerified(ctx, c, rel, "panel.tar.gz", []string{"checksum.txt", "checksums.txt"}, tarball, r)
		}},
		{Name: "Maintenance mode on", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, fmt.Sprintf(`cd %s && php artisan down`, shq(in.Dir)))
		}},
		{Name: "Unpack " + rel.Tag, Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, fmt.Sprintf(`cd %s && tar -xzf %s && rm -f %s
chmod -R 755 storage/* bootstrap/cache/`, shq(in.Dir), shq(tarball), shq(tarball)))
		}},
		{Name: "PHP dependencies (composer install)", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, fmt.Sprintf(`cd %s && chown -R www-data:www-data . && runuser -u www-data -- env HOME=/tmp COMPOSER_HOME=/tmp/composer composer install --no-dev --optimize-autoloader --no-interaction --quiet`, shq(in.Dir)))
		}},
		{Name: "Clear caches and migrate the database", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, fmt.Sprintf(`cd %s
php artisan optimize:clear >/dev/null
if php artisan list --raw 2>/dev/null | grep -q '^filament:optimize'; then php artisan filament:optimize >/dev/null; fi
php artisan migrate --seed --force
chown -R www-data:www-data .`, shq(in.Dir)))
		}},
		{Name: "Restart the queue worker, maintenance mode off", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, fmt.Sprintf(`cd %s && php artisan queue:restart >/dev/null
systemctl restart %s 2>/dev/null || true
php artisan up`, shq(in.Dir), queue))
		}},
	}
	err := r.Execute(ctx, steps)
	if err == nil && !r.DryRun {
		setState("PANEL_VERSION", strings.TrimPrefix(rel.Tag, "v"))
	}
	return b, err
}

// RestorePanel puts back the files and database from b.
func RestorePanel(ctx context.Context, b PanelBackup, r *Runner) error {
	if b.Dir == "" || !Exists(b.Dir+"/files") {
		return errors.New("no backup to restore")
	}
	restoreDB := ""
	if !b.SQLite && Exists(b.Dir+"/database.sql") {
		restoreDB = fmt.Sprintf(`db=$(sed -n 's/^DB_DATABASE="\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' %[1]s/files/.env); db=${db:-panel}
mariadb -e "DROP DATABASE IF EXISTS $db; CREATE DATABASE $db;"
mariadb "$db" < %[1]s/database.sql`, shq(b.Dir))
	}
	return r.Execute(ctx, []Step{{Name: "Restore the panel from " + b.Dir, Run: func(ctx context.Context, r *Runner) error {
		return r.Sh(ctx, fmt.Sprintf(`rm -rf %[1]s.failed && mv %[1]s %[1]s.failed
cp -a %[2]s/files %[1]s
%[3]s
cd %[1]s && php artisan queue:restart >/dev/null 2>&1 || true
systemctl restart pteroq pelican-queue 2>/dev/null || true
php artisan up
rm -rf %[1]s.failed`, shq(b.PanelDir), shq(b.Dir), restoreDB))
	}}})
}

// UpgradeWings replaces the Wings binary with rel and restarts it. Game
// servers keep running. If Wings does not come back, the old binary is put
// back.
func UpgradeWings(ctx context.Context, rel ghrel.Release, c *ghrel.Client, r *Runner) error {
	const bin = "/usr/local/bin/wings"
	return r.Execute(ctx, []Step{
		{Name: "Download Wings " + rel.Tag, Run: func(ctx context.Context, r *Runner) error {
			return fetchVerified(ctx, c, rel, wingsAsset(), []string{"checksums.txt", "checksum.txt"}, bin+".new", r)
		}},
		{Name: "Install Wings " + rel.Tag + " and restart it", Run: func(ctx context.Context, r *Runner) error {
			if err := r.Sh(ctx, fmt.Sprintf(`chmod 0755 %[1]s.new && cp -a %[1]s %[1]s.prev && mv %[1]s.new %[1]s
systemctl restart wings`, bin)); err != nil {
				return err
			}
			if r.DryRun {
				return nil
			}
			if err := waitWings(ctx, 90*time.Second); err != nil {
				r.log("Wings %s did not come back (%v); putting the old version back", rel.Tag, err)
				_ = r.Sh(ctx, fmt.Sprintf(`mv %[1]s.prev %[1]s && systemctl restart wings`, bin))
				return fmt.Errorf("wings %s did not start: %w; the old version is running again", rel.Tag, err)
			}
			return nil
		}},
	})
}

// fetchVerified downloads asset to dst and checks it against the release's
// checksum file (one of sums) or GitHub's digest; without either it warns.
func fetchVerified(ctx context.Context, c *ghrel.Client, rel ghrel.Release, asset string, sums []string, dst string, r *Runner) error {
	a, ok := rel.Assets[asset]
	if !ok {
		return fmt.Errorf("%s has no %s", rel.Tag, asset)
	}
	if r.DryRun {
		r.Script.WriteString(fmt.Sprintf("curl -fsSL -o %s %s\n", shq(dst), a.URL))
		return nil
	}
	want := a.SHA256
	for _, s := range sums {
		if sum, err := c.Checksum(ctx, rel, s, asset); err == nil && sum != "" {
			want = sum
			break
		}
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	got, err := c.Fetch(ctx, a.URL, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return err
	}
	switch {
	case want == "":
		r.log("    %s %s publishes no checksum; downloaded over HTTPS from GitHub (sha256 %s)", asset, rel.Tag, got)
	case !strings.EqualFold(want, got):
		os.Remove(dst)
		return fmt.Errorf("checksum mismatch for %s %s", asset, rel.Tag)
	default:
		r.log("    checksum ok")
	}
	return nil
}

// waitWings waits until the Wings service is active and its API answers.
func waitWings(ctx context.Context, timeout time.Duration) error {
	var wc struct {
		Token string `yaml:"token"`
		API   struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
			SSL  struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"ssl"`
		} `yaml:"api"`
	}
	for _, p := range []string{"/etc/pterodactyl/config.yml", "/etc/pelican/config.yml"} {
		if b, err := os.ReadFile(p); err == nil {
			_ = yaml.Unmarshal(b, &wc)
			break
		}
	}
	scheme := "http"
	if wc.API.SSL.Enabled {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://127.0.0.1:%d/api/system", scheme, wc.API.Port)
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // loopback; the certificate is for the public name
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if exec.Command("systemctl", "is-active", "--quiet", "wings").Run() == nil {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			req.Header.Set("Authorization", "Bearer "+wc.Token)
			req.Header.Set("Accept", "application/json")
			resp, err := hc.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
				err = fmt.Errorf("%s answered %s", url, resp.Status)
			}
			last = err
		} else {
			last = errors.New("the wings service is not active")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return last
}

// setState records one value in install.env.
func setState(k, v string) {
	st := loadState()
	if len(st) == 0 {
		return
	}
	st[k] = v
	_ = st.save()
}

// SetProduct records the installed product after a channel switch.
func SetProduct(product string) { setState("PRODUCT", product) }
