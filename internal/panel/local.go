package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Local runs Panel code through `php artisan tinker` on the panel host, for
// what Pterodactyl 1.x has no API for (API keys, egg import, mounts). It runs
// as the owner of the panel files so no root-owned cache or log files appear.
type Local struct {
	Dir   string // panel directory, e.g. /var/www/pterodactyl
	Owner string // user that owns the panel files
	Kind  Kind
}

// Known panel install locations.
var localDirs = map[string]Kind{"/var/www/pterodactyl": Pterodactyl1, "/var/www/pelican": Pelican}

// DetectLocal finds a panel installed on this host.
func DetectLocal() (*Local, bool) {
	for dir, kind := range localDirs {
		fi, err := os.Stat(filepath.Join(dir, "artisan"))
		if err != nil {
			continue
		}
		owner := "www-data"
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			if u, err := user.LookupId(strconv.Itoa(int(st.Uid))); err == nil {
				owner = u.Username
			}
		}
		if kind == Pterodactyl1 {
			// Pterodactyl 2 keeps the same directory; it has app/Models/Tag.php.
			if _, err := os.Stat(filepath.Join(dir, "app/Models/Tag.php")); err == nil {
				kind = Pterodactyl2
			}
		}
		return &Local{Dir: dir, Owner: owner, Kind: kind}, true
	}
	return nil, false
}

// ns is the PHP namespace of the panel's code.
func (l *Local) ns() string {
	if l.Kind == Pelican {
		return `\App`
	}
	return `\Pterodactyl`
}

// php runs code (which must assign $out) and returns $out as JSON.
func (l *Local) php(ctx context.Context, code string) (json.RawMessage, error) {
	wrapped := "try { " + code + " echo \"\\nEVRJSON:\" . json_encode($out) . \"\\n\"; } catch (\\Throwable $e) { echo \"\\nEVRERR:\" . str_replace(\"\\n\", \" \", $e->getMessage()) . \"\\n\"; }"
	// PsySH (tinker) writes config and history to $HOME, which for www-data
	// is usually not writable; give it a private temporary home.
	home, err := os.MkdirTemp("", "evr-tinker-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	args := []string{"env", "HOME=" + home, "XDG_CONFIG_HOME=" + home, "XDG_DATA_HOME=" + home, "php", "artisan", "tinker", "--execute=" + wrapped}
	var cmd *exec.Cmd
	if os.Geteuid() == 0 && l.Owner != "" && l.Owner != "root" {
		if u, err := user.Lookup(l.Owner); err == nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			_ = os.Chown(home, uid, gid)
		}
		cmd = exec.CommandContext(ctx, "runuser", append([]string{"-u", l.Owner, "--"}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, args[0], args[1:]...)
	}
	cmd.Dir = l.Dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	for _, line := range strings.Split(out.String(), "\n") {
		if v, ok := strings.CutPrefix(line, "EVRJSON:"); ok {
			return json.RawMessage(v), nil
		}
		if v, ok := strings.CutPrefix(line, "EVRERR:"); ok {
			return nil, fmt.Errorf("panel: %s", v)
		}
	}
	if runErr != nil {
		return nil, fmt.Errorf("artisan tinker: %v: %s", runErr, lastLines(out.String(), 3))
	}
	return nil, fmt.Errorf("artisan tinker gave no result: %s", lastLines(out.String(), 3))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

func phpString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func phpInts(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// CreateAPIKey creates an application key (app=true) or an account key for
// userID and returns the full token. Application keys get read/write on all
// resources.
func (l *Local) CreateAPIKey(ctx context.Context, userID int, app bool, memo string) (string, error) {
	if l.Kind != Pterodactyl1 && l.Kind != Pterodactyl2 {
		return "", errors.New("create API keys in the Pelican admin area")
	}
	typ, perms := 1, "[]"
	if app {
		typ = 2
		perms = "['r_servers'=>3,'r_nodes'=>3,'r_allocations'=>3,'r_users'=>3,'r_locations'=>3,'r_nests'=>3,'r_eggs'=>3,'r_database_hosts'=>3,'r_server_databases'=>3]"
	}
	code := fmt.Sprintf(`$k = app(%s\Services\Api\KeyCreationService::class)->setKeyType(%d)->handle(['user_id'=>%d,'memo'=>%s,'allowed_ips'=>[]], %s); $out = $k->identifier . decrypt($k->token);`,
		l.ns(), typ, userID, phpString(memo), perms)
	if l.Kind == Pterodactyl2 {
		// 2.0 replaced KeyCreationService with the CreatesApiKeys action and
		// dropped the nests permission.
		perms = strings.Replace(perms, "'r_nests'=>3,", "", 1)
		code = fmt.Sprintf(`$k = app(\Pterodactyl\Contracts\Api\CreatesApiKeys::class)->create(%d, ['user_id'=>%d,'memo'=>%s], %s); $out = $k->identifier . decrypt($k->token);`,
			typ, userID, phpString(memo), perms)
	}
	raw, err := l.php(ctx, code)
	if err != nil {
		return "", err
	}
	var key string
	return key, json.Unmarshal(raw, &key)
}

// AdminUserID returns the first root admin's ID.
func (l *Local) AdminUserID(ctx context.Context) (int, error) {
	raw, err := l.php(ctx, fmt.Sprintf(`$out = %s\Models\User::query()->where('root_admin', true)->orderBy('id')->value('id');`, l.ns()))
	if err != nil {
		return 0, err
	}
	var id int
	if err := json.Unmarshal(raw, &id); err != nil || id == 0 {
		return 0, errors.New("the panel has no admin user")
	}
	return id, nil
}

// ImportEgg imports the egg into the named nest (created if missing), or
// updates the egg of the same name in that nest. Returns the egg ID.
func (l *Local) ImportEgg(ctx context.Context, nestName, eggName string, data []byte) (int, error) {
	if l.Kind != Pterodactyl1 {
		return 0, errors.New("use the API to import eggs on this panel")
	}
	f, err := os.CreateTemp("", "evr-egg-*.json")
	if err != nil {
		return 0, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return 0, err
	}
	f.Close()
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		return 0, err
	}
	ns := l.ns()
	code := fmt.Sprintf(`$nest = %[1]s\Models\Nest::query()->where('name', %[2]s)->first();
if (!$nest) { $nest = app(%[1]s\Services\Nests\NestCreationService::class)->handle(['name'=>%[2]s, 'description'=>'Echo VR dedicated servers (evr-ptero)']); }
$file = new \Illuminate\Http\UploadedFile(%[3]s, 'egg.json', 'application/json', null, true);
$egg = %[1]s\Models\Egg::query()->where('nest_id', $nest->id)->where('name', %[4]s)->first();
if ($egg) { $egg = app(%[1]s\Services\Eggs\Sharing\EggUpdateImporterService::class)->handle($egg, $file); }
else { $egg = app(%[1]s\Services\Eggs\Sharing\EggImporterService::class)->handle($file, $nest->id); }
$out = $egg->id;`, ns, phpString(nestName), phpString(f.Name()), phpString(eggName))
	raw, err := l.php(ctx, code)
	if err != nil {
		return 0, err
	}
	var id int
	return id, json.Unmarshal(raw, &id)
}

// EnsureMount creates a read-only mount (or finds it by name) and attaches it
// to eggs and nodes. Returns the mount ID.
func (l *Local) EnsureMount(ctx context.Context, name, source, target string, eggs, nodes []int) (int, error) {
	if l.Kind != Pterodactyl1 {
		return 0, errors.New("use the API to manage mounts on this panel")
	}
	code := fmt.Sprintf(`$m = %[1]s\Models\Mount::query()->where('name', %[2]s)->first();
if (!$m) { $m = (new %[1]s\Models\Mount())->fill(['name'=>%[2]s, 'description'=>'Echo VR shared files (evr-ptero)', 'source'=>%[3]s, 'target'=>%[4]s, 'read_only'=>true, 'user_mountable'=>false]); $m->forceFill(['uuid' => \Ramsey\Uuid\Uuid::uuid4()->toString()]); $m->saveOrFail(); }
else { $m->forceFill(['source'=>%[3]s, 'target'=>%[4]s, 'read_only'=>true])->saveOrFail(); }
$m->eggs()->syncWithoutDetaching(%[5]s); $m->nodes()->syncWithoutDetaching(%[6]s);
$out = $m->id;`, l.ns(), phpString(name), phpString(source), phpString(target), phpInts(eggs), phpInts(nodes))
	raw, err := l.php(ctx, code)
	if err != nil {
		return 0, err
	}
	var id int
	return id, json.Unmarshal(raw, &id)
}

// AttachMount attaches mounts to a server (Pterodactyl 1.x).
func (l *Local) AttachMount(ctx context.Context, serverID int, mounts []int) error {
	// MountServer is not mass-assignable; write the pivot rows directly.
	code := fmt.Sprintf(`foreach (%[1]s as $mid) { \Illuminate\Support\Facades\DB::table('mount_server')->insertOrIgnore(['server_id'=>%[2]d, 'mount_id'=>$mid]); } $out = true;`,
		phpInts(mounts), serverID)
	_, err := l.php(ctx, code)
	return err
}

// DetachMounts removes every mount from a server except keep.
func (l *Local) DetachMounts(ctx context.Context, serverID int, keep []int) error {
	code := fmt.Sprintf(`foreach (%[1]s\Models\Mount::query()->whereNotIn('id', %[2]s)->get() as $m) { $m->servers()->detach(%[3]d); } $out = true;`,
		l.ns(), phpInts(keep), serverID)
	_, err := l.php(ctx, code)
	return err
}

// RemoveLegacy deletes the mounts whose source is one of sources, and the
// eggs that no server uses any more. It returns what was deleted.
func (l *Local) RemoveLegacy(ctx context.Context, eggIDs []int, sources []string) (eggs, mounts []string, err error) {
	quoted := make([]string, len(sources))
	for i, s := range sources {
		quoted[i] = phpString(s)
	}
	code := fmt.Sprintf(`$out = ['eggs' => [], 'mounts' => []];
foreach (%[1]s\Models\Mount::query()->whereIn('source', [%[2]s])->get() as $m) { $m->eggs()->detach(); $m->nodes()->detach(); $m->servers()->detach(); $out['mounts'][] = $m->name; $m->delete(); }
foreach (%[1]s\Models\Egg::query()->whereIn('id', %[3]s)->get() as $e) { if ($e->servers()->count() > 0) continue; $e->variables()->delete(); $out['eggs'][] = $e->name; $e->delete(); }`,
		l.ns(), strings.Join(quoted, ", "), phpInts(eggIDs))
	raw, err := l.php(ctx, code)
	if err != nil {
		return nil, nil, err
	}
	var res struct{ Eggs, Mounts []string }
	err = json.Unmarshal(raw, &res)
	return res.Eggs, res.Mounts, err
}

// PushNodeConfig sends a node's current configuration (incl. allowed mounts)
// to its Wings, which applies it without a restart.
func (l *Local) PushNodeConfig(ctx context.Context, nodeID int) error {
	code := fmt.Sprintf(`$n = %[1]s\Models\Node::query()->findOrFail(%[2]d); app(%[1]s\Repositories\Wings\DaemonConfigurationRepository::class)->setNode($n)->update($n); $out = true;`, l.ns(), nodeID)
	_, err := l.php(ctx, code)
	return err
}
