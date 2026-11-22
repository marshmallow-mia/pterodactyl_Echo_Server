package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// A server's own startup command (e.g. with EVR_TELEMETRY_DUMP=1) survives a
// change of its settings; it used to be reset to the egg's.
func TestUpdateServerKeepsStartup(t *testing.T) {
	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/application/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"attributes":{"id":1,"uuid":"s-1","identifier":"abc","name":"Echo VR 1","egg":5,
			"container":{"image":"img","startup_command":"env EVR_TELEMETRY_DUMP=1 evr-supervisor","environment":{"NEVR_REGIONS":"euw"}}}}],"meta":{"pagination":{"total_pages":1}}}`))
	})
	mux.HandleFunc("PATCH /api/application/servers/1/startup", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		got, _ = body["startup"].(string)
		w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := pcfg.New()
	cfg.Panel.URL, cfg.Panel.Kind, cfg.Panel.EggID, cfg.Panel.WingsData = srv.URL, "pelican", 5, t.TempDir()
	a := &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{}}
	ctx := context.Background()
	s, err := a.Find(ctx, "Echo VR 1")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateServer(ctx, s, func(set *instance.Settings) { set.TickRate = 60 }, false); err != nil {
		t.Fatal(err)
	}
	if got != "env EVR_TELEMETRY_DUMP=1 evr-supervisor" {
		t.Errorf("startup %q", got)
	}
}
