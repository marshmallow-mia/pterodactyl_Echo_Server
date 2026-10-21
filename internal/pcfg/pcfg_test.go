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
