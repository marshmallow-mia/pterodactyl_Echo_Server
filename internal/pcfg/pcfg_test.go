package pcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOldRuntimeAutoUpdateMoves(t *testing.T) {
	dir := t.TempDir()
	old := "version: 1\npanel:\n    url: https://p\n    kind: pterodactyl2\ndata_dir: /srv/echovr\ngame_source: mount\nruntime:\n    channel: pre\n    auto_update: false\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(old), 0o644)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	u := c.Updates
	if u.Runtime != "notify" || u.Self != "notify" || u.Panel != "notify" || u.Wings != "notify" || u.Game != "notify" || u.CheckHours != 0 {
		t.Errorf("%+v", u)
	}
	if err := c.Save(dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if strings.Contains(string(b), "auto_update") {
		t.Errorf("saved:\n%s", b)
	}
	c.Updates.PanelChannel = "nightly"
	if err := c.Validate(); err == nil {
		t.Error("bad panel channel accepted")
	}
}

// A config from before runtime.updater keeps evr-ptero updating, on nEVR's
// stable channel (no nEVR channel follows GitHub's releases); a pin stays.
func TestOldRuntimeChannelMigrates(t *testing.T) {
	for _, c := range []struct{ in, channel, pinned string }{
		{"channel: pre", "stable", ""},
		{"channel: stable", "stable", ""},
		{"channel: pinned\n  pinned: v6.17.0", "pinned", "v6.17.0"},
	} {
		dir := t.TempDir()
		old := "version: 1\ndata_dir: /srv/echovr\nruntime:\n  " + c.in + "\n"
		os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(old), 0o644)
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if r := cfg.Runtime; r.Updater != UpdaterHost || r.Channel != c.channel || r.Pinned != c.pinned {
			t.Errorf("%q → %+v", c.in, r)
		}
		if env := cfg.Runtime.SelfUpdateEnv(); env["EVR_RUNTIME_UPDATER"] != "host" || env["NEVR_UPDATE_CHANNEL"] != "stable" {
			t.Errorf("env %v", env)
		}
	}
	if r := New().Runtime; r.Updater != UpdaterNevr || r.Channel != "stable" {
		t.Errorf("new config: %+v", r)
	}
}
