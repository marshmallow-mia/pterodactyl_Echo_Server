package panelinstall

import (
	"context"
	"fmt"
	"strings"
)

// UninstallOptions control what Uninstall removes.
type UninstallOptions struct {
	// Packages also purges PHP, MariaDB, Redis, nginx and certbot.
	Packages bool
	// Docker also purges Docker and every container and image.
	Docker bool
	// Certificates also deletes /etc/letsencrypt. They are kept by default:
	// Let's Encrypt limits duplicate certificates to 5 per week.
	Certificates bool
}

// Uninstall removes the panel, its database, Wings and all server data.
// Servers are stopped and their files are deleted; this cannot be undone.
func Uninstall(ctx context.Context, o UninstallOptions, r *Runner) error {
	steps := []Step{
		{Name: "Stop services", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `for u in evr-ptero-agent wings pteroq pelican-queue; do systemctl disable --now $u >/dev/null 2>&1 || true; done
rm -f /etc/systemd/system/evr-ptero-agent.service /etc/systemd/system/wings.service /etc/systemd/system/pteroq.service /etc/systemd/system/pelican-queue.service
systemctl daemon-reload`)
		}},
		{Name: "Remove game server containers", Done: func(context.Context) bool { return !Has("docker") }, Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `ids=$(docker ps -aq --filter label=Service=Pterodactyl; docker ps -aq --filter label=Service=Pelican)
[ -z "$ids" ] || docker rm -f $ids >/dev/null
docker network rm pterodactyl_nw pelican_nw >/dev/null 2>&1 || true`)
		}},
		{Name: "Remove the panel and Wings", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `if command -v mariadb >/dev/null && systemctl is-active --quiet mariadb; then
  mariadb -e "DROP DATABASE IF EXISTS panel; DROP USER IF EXISTS 'pterodactyl'@'127.0.0.1'; DROP USER IF EXISTS 'pelican'@'127.0.0.1';"
fi
rm -rf /var/www/pterodactyl /var/www/pelican /etc/nginx/sites-enabled/pterodactyl.conf /etc/nginx/sites-available/pterodactyl.conf /etc/nginx/sites-enabled/pelican.conf /etc/nginx/sites-available/pelican.conf
rm -f /etc/cron.d/pterodactyl; crontab -u www-data -r 2>/dev/null || true
systemctl reload nginx 2>/dev/null || true
rm -rf /usr/local/bin/wings /etc/pterodactyl /etc/pelican /var/lib/pterodactyl /var/lib/pelican /var/log/pterodactyl /var/log/pelican /tmp/pterodactyl /tmp/pelican /var/run/wings
userdel pterodactyl 2>/dev/null || true; userdel pelican 2>/dev/null || true
rm -f /etc/evr-ptero/install.env /etc/evr-ptero/config.yaml /etc/evr-ptero/secrets.env`)
		}},
	}
	if o.Packages {
		steps = append(steps, Step{Name: "Purge PHP, MariaDB, Redis, nginx, certbot", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `pk=$(dpkg-query -W -f='${Package}\n' | grep -E '^(php|mariadb|mysql|galera|redis|nginx|certbot|python3-certbot)' | tr '\n' ' ')
[ -z "$pk" ] || apt-get purge -y -qq $pk >/dev/null
apt-get autoremove -y -qq --purge >/dev/null
rm -rf /etc/nginx /var/lib/mysql /etc/mysql /etc/php /var/lib/php /etc/redis /var/lib/redis /usr/local/bin/composer /etc/apt/sources.list.d/php.list /var/www/letsencrypt`)
		}})
	}
	if o.Docker {
		steps = append(steps, Step{Name: "Purge Docker", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `docker ps -aq | xargs -r docker rm -f >/dev/null 2>&1 || true
pk=$(dpkg-query -W -f='${Package}\n' | grep -E '^(docker-|containerd)' | tr '\n' ' ')
[ -z "$pk" ] || apt-get purge -y -qq $pk >/dev/null
rm -rf /var/lib/docker /var/lib/containerd /etc/docker /etc/apt/sources.list.d/docker.list`)
		}})
	}
	if o.Certificates {
		steps = append(steps, Step{Name: "Delete TLS certificates", Run: func(ctx context.Context, r *Runner) error {
			return r.Sh(ctx, `rm -rf /etc/letsencrypt /var/lib/letsencrypt /var/log/letsencrypt`)
		}})
	}
	return r.Execute(ctx, steps)
}

// Ports parses "444,443".
func Ports(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		var n int
		if _, err := fmtSscan(strings.TrimSpace(p), &n); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func fmtSscan(s string, n *int) (int, error) { return fmt.Sscan(s, n) }
