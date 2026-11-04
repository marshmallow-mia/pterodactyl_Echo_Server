package panel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// PluginResult says what InstallPlugin did.
type PluginResult struct {
	Action  string // "installed", "updated", "up to date"
	Version string
	Old     string // the version it replaced
}

// SupportsPlugins reports whether the panel has Pelican's plugin system.
func (l *Local) SupportsPlugins() bool {
	if l.Kind != Pelican {
		return false
	}
	_, err := os.Stat(filepath.Join(l.Dir, "app", "Models", "Plugin.php"))
	return err == nil
}

// PluginVersion is the installed version of plugin id and its panel status
// ("" when it is not there).
func (l *Local) PluginVersion(id string) (version, status string) {
	m, err := readManifest(filepath.Join(l.Dir, "plugins", id, "plugin.json"))
	if err != nil {
		return "", ""
	}
	version, _ = m["version"].(string)
	if meta, ok := m["meta"].(map[string]any); ok {
		status, _ = meta["status"].(string)
	}
	return version, status
}

// InstallPlugin puts the plugin tree src into the panel's plugins/<id>,
// keeping the panel's own state (plugin.json "meta": enabled, load order) on an
// update, and installs it with p:plugin:install the first time. Files the
// panel already has unchanged are left alone.
func (l *Local) InstallPlugin(ctx context.Context, id string, src fs.FS) (PluginResult, error) {
	if !l.SupportsPlugins() {
		return PluginResult{}, errors.New("this panel has no plugin system (Pelican 1.0 or newer)")
	}
	newManifest, err := fsManifest(src)
	if err != nil {
		return PluginResult{}, err
	}
	res := PluginResult{Action: "installed"}
	res.Version, _ = newManifest["version"].(string)
	dest := filepath.Join(l.Dir, "plugins", id)
	oldManifest, oldErr := readManifest(filepath.Join(dest, "plugin.json"))
	status := ""
	if oldErr == nil {
		res.Old, _ = oldManifest["version"].(string)
		if meta, ok := oldManifest["meta"].(map[string]any); ok {
			newManifest["meta"] = meta
			status, _ = meta["status"].(string)
		}
		same, err := sameTree(src, dest)
		if err != nil {
			return res, err
		}
		if same && status != "" && status != "not_installed" {
			res.Action = "up to date"
			return res, nil
		}
		res.Action = "updated"
	}
	uid, gid := l.ownerIDs()
	tmp := filepath.Join(l.Dir, "plugins", "."+id+".new")
	_ = os.RemoveAll(tmp)
	if err := copyTree(src, tmp, uid, gid); err != nil {
		os.RemoveAll(tmp)
		return res, err
	}
	b, _ := json.MarshalIndent(newManifest, "", "    ")
	if err := os.WriteFile(filepath.Join(tmp, "plugin.json"), append(b, '\n'), 0o644); err != nil {
		os.RemoveAll(tmp)
		return res, err
	}
	_ = os.Lchown(filepath.Join(tmp, "plugin.json"), uid, gid)
	old := filepath.Join(l.Dir, "plugins", "."+id+".old")
	_ = os.RemoveAll(old)
	if oldErr == nil {
		if err := os.Rename(dest, old); err != nil {
			os.RemoveAll(tmp)
			return res, err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		if oldErr == nil {
			_ = os.Rename(old, dest)
		}
		return res, err
	}
	_ = os.RemoveAll(old)

	script := ""
	if status == "" || status == "not_installed" {
		script = l.artisan("p:plugin:install "+id) + "\n"
	}
	// Discovered pages and widgets are cached; Blade views are compiled.
	script += l.artisan("view:clear") + " >/dev/null\n" +
		"if [ -d bootstrap/cache/filament ]; then " + l.artisan("filament:optimize-clear") + " >/dev/null && " + l.artisan("filament:optimize") + " >/dev/null; fi"
	if out, err := l.sh(ctx, script); err != nil {
		return res, fmt.Errorf("%v: %s", err, lastLines(out, 8))
	}
	if v, st := l.PluginVersion(id); st != "enabled" {
		return res, fmt.Errorf("plugin %s %s is %q in the panel, not enabled (Admin → Plugins says why)", id, v, st)
	}
	return res, nil
}

// UninstallPlugin removes plugin id from the panel, files included.
func (l *Local) UninstallPlugin(ctx context.Context, id string) error {
	if v, _ := l.PluginVersion(id); v == "" {
		return nil
	}
	out, err := l.sh(ctx, l.artisan("p:plugin:uninstall "+id+" --delete")+"\n"+
		"if [ -d bootstrap/cache/filament ]; then "+l.artisan("filament:optimize-clear")+" >/dev/null && "+l.artisan("filament:optimize")+" >/dev/null; fi")
	if err != nil {
		return fmt.Errorf("%v: %s", err, lastLines(out, 8))
	}
	return nil
}

func (l *Local) artisan(args string) string {
	return fmt.Sprintf("runuser -u %s -- env HOME=/tmp php artisan %s -n", l.Owner, args)
}

func (l *Local) sh(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", "set -euo pipefail\ncd "+shellQuote(l.Dir)+"\n"+script)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (l *Local) ownerIDs() (int, int) {
	u, err := user.Lookup(l.Owner)
	if err != nil {
		return os.Getuid(), os.Getgid()
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid
}

func readManifest(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	return m, json.Unmarshal(b, &m)
}

func fsManifest(src fs.FS) (map[string]any, error) {
	b, err := fs.ReadFile(src, "plugin.json")
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	return m, json.Unmarshal(b, &m)
}

// treeHash hashes every file but plugin.json, whose "meta" the panel owns.
func treeHash(fsys fs.FS) (string, error) {
	var names []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && p != "plugin.json" {
			names = append(names, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(b))
		h.Write(b)
	}
	if m, err := fsManifest(fsys); err == nil {
		delete(m, "meta")
		b, _ := json.Marshal(m)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sameTree(src fs.FS, dir string) (bool, error) {
	a, err := treeHash(src)
	if err != nil {
		return false, err
	}
	b, err := treeHash(os.DirFS(dir))
	if err != nil {
		return false, nil
	}
	return a == b, nil
}

func copyTree(src fs.FS, dst string, uid, gid int) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			return os.Lchown(target, uid, gid)
		}
		if strings.HasSuffix(p, ".go") {
			return nil // the embedding package, not part of the plugin
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
		return os.Lchown(target, uid, gid)
	})
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
