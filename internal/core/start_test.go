package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// panelAnswering serves the client power endpoint with the given statuses in
// turn (the last one repeats).
func panelAnswering(t *testing.T, statuses ...int) (*App, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/power") {
			http.NotFound(w, r)
			return
		}
		n := int(calls.Add(1)) - 1
		code := statuses[min(n, len(statuses)-1)]
		w.WriteHeader(code)
		if code >= 400 {
			w.Write([]byte(`{"errors":[{"detail":"An unexpected error was encountered"}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	old := retryWait
	retryWait = time.Millisecond
	t.Cleanup(func() { retryWait = old })
	cfg := &pcfg.Config{GameSource: "download"}
	cfg.Panel.URL, cfg.Panel.Kind = srv.URL, panel.Pelican
	return &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{pcfg.SecretClientKey: "k"}}, &calls
}

func TestStartRetriesWhileWingsIsUnreachable(t *testing.T) {
	a, calls := panelAnswering(t, 500, 500, 204)
	if err := a.startWhenInstalled(context.Background(), panel.Server{Name: "Echo VR 1", Identifier: "abc"}, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("%d calls", calls.Load())
	}
}

func TestStartGivesUpOnOtherErrors(t *testing.T) {
	a, calls := panelAnswering(t, 403)
	err := a.startWhenInstalled(context.Background(), panel.Server{Name: "Echo VR 1", Identifier: "abc"}, nil)
	if err == nil || !strings.Contains(err.Error(), "Echo VR 1") || calls.Load() != 1 {
		t.Fatalf("err %v after %d calls", err, calls.Load())
	}
}
