package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/status"

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

// The runtime policy is this node's (its shared files): a server the panel
// runs on another node keeps its environment and isn't restarted. Its status
// isn't on this node, so a restart couldn't wait for its match either.
func TestApplyRuntimePolicyOnlyOnThisNode(t *testing.T) {
	var mu sync.Mutex
	var touched []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/application/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[
			{"attributes":{"id":1,"uuid":"s-1","identifier":"abc","name":"Echo VR 1","egg":5,"node":1,"container":{"image":"img","environment":{}}}},
			{"attributes":{"id":2,"uuid":"s-2","identifier":"def","name":"Echo VR 2","egg":5,"node":2,"container":{"image":"img","environment":{}}}}
		],"meta":{"pagination":{"total_pages":1}}}`))
	})
	mux.HandleFunc("PATCH /api/application/servers/{id}/startup", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		touched = append(touched, "env "+r.PathValue("id"))
		mu.Unlock()
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /api/client/servers/{id}/resources", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"attributes":{"current_state":"running"}}`))
	})
	mux.HandleFunc("POST /api/client/servers/{id}/power", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		touched = append(touched, "power "+r.PathValue("id"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := pcfg.New()
	wings := t.TempDir()
	cfg.Panel.URL, cfg.Panel.Kind, cfg.Panel.EggID, cfg.Panel.WingsData, cfg.Panel.NodeID = srv.URL, "pelican", 5, wings, 1
	a := &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{pcfg.SecretAppKey: "app", pcfg.SecretClientKey: "client"}}
	setState(t, wings, "s-1", status.StateRegistered)

	if err := a.ApplyRuntimePolicy(context.Background(), true, func(string) {}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(touched, ","); got != "env 1,power abc" {
		t.Errorf("touched %q, want only Echo VR 1 (this node's)", got)
	}
}
