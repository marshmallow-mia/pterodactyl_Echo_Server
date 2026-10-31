package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/mods"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// storeVersion puts a mod version into a store the way Store.Install leaves it.
func storeVersion(t *testing.T, dir, id, ver string, files map[string]string) {
	t.Helper()
	in := mods.Installed{ID: id, Version: ver, Files: map[string]string{}}
	for rel, body := range files {
		p := filepath.Join(dir, id, ver, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
		sum := sha256.Sum256([]byte(body))
		in.Files[rel] = hex.EncodeToString(sum[:])
	}
	b, _ := json.Marshal(in)
	os.WriteFile(filepath.Join(dir, id, ver, "manifest.json"), b, 0o644)
}

func TestModLockGoesIntoTheVolume(t *testing.T) {
	cfg := pcfg.New()
	cfg.DataDir, cfg.Panel.WingsData = t.TempDir(), t.TempDir()
	a := &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{}}
	vol := filepath.Join(cfg.Panel.WingsData, "uuid-1")
	os.MkdirAll(vol, 0o755)
	modServers.Store("echo-1", "uuid-1")
	storeVersion(t, cfg.ModsDir(), "server-patches", "0.4.1", map[string]string{"plugins/NvrServerPatches.dll": "patches"})
	storeVersion(t, cfg.ModsDir(), "server-patches", "0.4.2", map[string]string{"plugins/NvrServerPatches.dll": "patches 2"})
	f := &modFleet{a: a}
	lock := mods.Lock{Mods: []mods.Locked{{ID: "server-patches", Version: "0.4.1", Plugins: []string{"NvrServerPatches.dll"}}}}
	if err := f.WriteLock("echo-1", lock); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(vol, "mods/store/server-patches/0.4.1/plugins/NvrServerPatches.dll")); err != nil || string(b) != "patches" {
		t.Fatalf("copy: %q %v", b, err)
	}
	if got, err := f.ReadLock("echo-1"); err != nil || !got.Same(lock) {
		t.Errorf("lock read back: %+v %v", got, err)
	}
	// A new version replaces the old copy.
	lock.Mods[0].Version = "0.4.2"
	if err := f.WriteLock("echo-1", lock); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vol, "mods/store/server-patches/0.4.1")); !os.IsNotExist(err) {
		t.Error("the old version's copy stayed")
	}
	// The supervisor reads this copy as its store: it must check out.
	in, err := (mods.Store{Dir: filepath.Join(vol, "mods/store")}).Get("server-patches", "0.4.2")
	if err != nil || in.Files["plugins/NvrServerPatches.dll"] == "" {
		t.Errorf("copied manifest: %+v %v", in, err)
	}
	// A link the server put into its volume can't lead the copy outside it.
	outside := t.TempDir()
	os.RemoveAll(filepath.Join(vol, "mods"))
	if err := os.Symlink(outside, filepath.Join(vol, "mods")); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteLock("echo-1", lock); err == nil {
		t.Error("wrote through a link out of the volume")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("files outside the volume: %v", ents)
	}
	os.Remove(filepath.Join(vol, "mods"))
	if err := os.Symlink(filepath.Join(outside, "lock"), filepath.Join(vol, LockName+".tmp")); err != nil {
		t.Fatal(err)
	}
	f.WriteLock("echo-1", lock)
	if _, err := os.Stat(filepath.Join(outside, "lock")); !os.IsNotExist(err) {
		t.Error("the lock was written through a link out of the volume")
	}
}

func TestModEditsByServer(t *testing.T) {
	cfg := pcfg.New()
	cfg.DataDir = t.TempDir()
	a := &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{}}
	if err := a.AddMod(t.Context(), "", mods.Spec{ID: "server-patches"}); err != nil {
		t.Fatal(err)
	}
	if err := a.AddMod(t.Context(), "", mods.Spec{ID: "x", Args: map[string]string{"k": "${HOME}"}}); err == nil {
		t.Error("an argument with ${...} was saved")
	}
	if len(a.Cfg.Defaults.Mods) != 1 {
		t.Errorf("a refused edit stayed: %+v", a.Cfg.Defaults.Mods)
	}
	re, err := pcfg.Load(a.Dir)
	if err != nil || len(re.Defaults.Mods) != 1 || re.Defaults.Mods[0].ID != "server-patches" {
		t.Errorf("saved: %+v %v", re.Defaults.Mods, err)
	}
}
