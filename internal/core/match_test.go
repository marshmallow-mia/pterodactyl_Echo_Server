package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/status"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// fakeFleet serves two running Echo VR servers (abc idle, def in a match) and
// records the power signals sent to them.
func fakeFleet(t *testing.T, wings string) (*App, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var power []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/application/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[
			{"attributes":{"id":1,"uuid":"s-1","identifier":"abc","name":"Echo VR 1","egg":5,"container":{"image":"img","environment":{}}}},
			{"attributes":{"id":2,"uuid":"s-2","identifier":"def","name":"Echo VR 2","egg":5,"container":{"image":"img","environment":{}}}}
		],"meta":{"pagination":{"total_pages":1}}}`))
	})
	mux.HandleFunc("PATCH /api/application/servers/{id}/startup", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /api/client/servers/{id}/resources", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"attributes":{"current_state":"running"}}`))
	})
	mux.HandleFunc("POST /api/client/servers/{id}/power", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		power = append(power, r.PathValue("id"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := pcfg.New()
	cfg.Panel.URL, cfg.Panel.Kind, cfg.Panel.EggID, cfg.Panel.WingsData = srv.URL, "pelican", 5, wings
	a := &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{pcfg.SecretAppKey: "app", pcfg.SecretClientKey: "client"}}
	setState(t, wings, "s-1", status.StateRegistered)
	setState(t, wings, "s-2", status.StateInSession)
	return a, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), power...)
	}
}

func setState(t *testing.T, wings, uuid string, st status.State) {
	t.Helper()
	os.MkdirAll(filepath.Join(wings, uuid), 0o755)
	if err := status.Write(filepath.Join(wings, uuid, status.FileName), status.Status{State: st}); err != nil {
		t.Fatal(err)
	}
}

func shortMatchWait(t *testing.T, wait time.Duration) {
	oldWait, oldPoll := MatchWait, matchPoll
	MatchWait, matchPoll = wait, 10*time.Millisecond
	t.Cleanup(func() { MatchWait, matchPoll = oldWait, oldPoll })
}

func TestApplyAccountRestartsAfterTheMatch(t *testing.T) {
	shortMatchWait(t, 5*time.Second)
	wings := t.TempDir()
	a, power := fakeFleet(t, wings)
	go func() {
		time.Sleep(200 * time.Millisecond)
		if got := power(); len(got) != 1 || got[0] != "abc" {
			t.Errorf("before the match ended: power %v, want only the idle server", got)
		}
		setState(t, wings, "s-2", status.StateRegistered)
	}()
	var said []string
	if err := a.ApplyAccount(context.Background(), func(s string) { said = append(said, s) }); err != nil {
		t.Fatal(err)
	}
	if got := power(); len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Errorf("power %v", got)
	}
	want := []string{"Echo VR 1: updated, restarting", "Echo VR 2: updated; in a match, restarts when it ends", "Echo VR 2: match over, restarting"}
	if strings.Join(said, "|") != strings.Join(want, "|") {
		t.Errorf("said %q", said)
	}
}

func TestApplyAccountGivesUpOnALongMatch(t *testing.T) {
	shortMatchWait(t, 100*time.Millisecond)
	a, power := fakeFleet(t, t.TempDir())
	err := a.ApplyAccount(context.Background(), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "Echo VR 2 not restarted") || strings.Contains(err.Error(), "Echo VR 1") {
		t.Fatalf("err %v", err)
	}
	if got := power(); len(got) != 1 || got[0] != "abc" {
		t.Errorf("power %v: the server in a match was restarted", got)
	}
}

func TestApplyAccountStopsWaitingOnCancel(t *testing.T) {
	shortMatchWait(t, time.Minute)
	a, power := fakeFleet(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.ApplyAccount(ctx, func(string) {}) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting after the context ended")
	}
	if err == nil || !strings.Contains(err.Error(), "Echo VR 2 not restarted") {
		t.Fatalf("err %v", err)
	}
	if got := power(); len(got) != 1 {
		t.Errorf("power %v", got)
	}
}
