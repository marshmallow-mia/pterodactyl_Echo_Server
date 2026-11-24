package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// installed puts a fake installed runtime into a's shared files.
func installed(t *testing.T, a *App, in nevr.Installed) {
	t.Helper()
	dir := filepath.Join(a.Cfg.NevrDir(), in.Tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(in)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Picking a version keeps the servers on it, as the launcher's picker does:
// it becomes current and the policy is pinned to it with evr-ptero updating,
// so neither the next update check nor nEVR's own update moves them away.
func TestStayOnPinsWhatItPicks(t *testing.T) {
	cfg := pcfg.New()
	cfg.DataDir = t.TempDir()
	cfg.Runtime.Updater, cfg.Runtime.Channel = pcfg.UpdaterNevr, "beta"
	a := &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{}}
	installed(t, a, nevr.Installed{Tag: "v6.17.0"})
	installed(t, a, nevr.Installed{Tag: "v6.16.5"})
	installed(t, a, nevr.Installed{Tag: "v6.15.0", Bad: "canary failed"})
	if err := a.Store().Use("v6.17.0"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := a.StayOn(ctx, "v6.16.5"); err != nil {
		t.Fatal(err)
	}
	r := a.Cfg.Runtime
	if cur := a.Store().Current(); cur != "v6.16.5" || r.Channel != "pinned" || r.Pinned != "v6.16.5" || r.Updater != pcfg.UpdaterHost {
		t.Fatalf("after StayOn: current %s, runtime %+v", cur, r)
	}
	saved, err := pcfg.Load(a.Dir)
	if err != nil || saved.Runtime.Channel != "pinned" || saved.Runtime.Pinned != "v6.16.5" || saved.Runtime.Updater != pcfg.UpdaterHost {
		t.Fatalf("saved %+v (%v)", saved.Runtime, err)
	}
	if env := a.Cfg.Runtime.SelfUpdateEnv(); env["EVR_RUNTIME_UPDATER"] != nevr.UpdaterHost {
		t.Fatalf("servers' environment after StayOn: %v", env)
	}

	// A rollback stays too: back on 6.17.0, pinned to it.
	if tag, err := a.StayOnPrevious(ctx); err != nil || tag != "v6.17.0" {
		t.Fatalf("StayOnPrevious = %s, %v", tag, err)
	}
	if cur := a.Store().Current(); cur != "v6.17.0" || a.Cfg.Runtime.Pinned != "v6.17.0" {
		t.Fatalf("after rollback: current %s, pinned %s", cur, a.Cfg.Runtime.Pinned)
	}

	// A version whose rollout failed is refused, and nothing changes.
	if err := a.StayOn(ctx, "v6.15.0"); err == nil {
		t.Fatal("StayOn took a version marked bad")
	}
	if cur := a.Store().Current(); cur != "v6.17.0" || a.Cfg.Runtime.Pinned != "v6.17.0" {
		t.Fatalf("after a refused pick: current %s, pinned %s", cur, a.Cfg.Runtime.Pinned)
	}
}
