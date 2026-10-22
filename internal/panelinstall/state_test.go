package panelinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateFollowsConfigDir(t *testing.T) {
	dir := t.TempDir()
	defer SetStateDir(stateDir)
	SetStateDir(dir)
	if err := (state{"ADMIN_USER": "admin"}).save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "install.env")); err != nil {
		t.Fatalf("install.env not in the config dir: %v", err)
	}
	if got := loadState()["ADMIN_USER"]; got != "admin" {
		t.Fatalf("read back %q", got)
	}
}
