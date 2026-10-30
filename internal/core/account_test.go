package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

func fakeBackend(t *testing.T) *httptest.Server {
	t.Helper()
	claims, _ := json.Marshal(map[string]any{"uid": "u-1", "usn": "operator", "exp": time.Now().Add(time.Hour).Unix()})
	tok := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/v2/rpc/account/authenticate/password" || body["password"] != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"token": tok, "refresh_token": "r-1"})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakePanel serves one Echo VR server and records the startup environment.
func fakePanel(t *testing.T, got *map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/application/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"attributes":{"id":1,"uuid":"s-1","identifier":"abc","name":"Echo VR 1","egg":5,
			"container":{"image":"img","environment":{"NEVR_REGIONS":"euw"}}}}],"meta":{"pagination":{"total_pages":1}}}`))
	})
	mux.HandleFunc("PATCH /api/application/servers/1/startup", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		*got = body["environment"].(map[string]any)
		w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestUsePasswordReplacesDeviceLink(t *testing.T) {
	var env map[string]any
	dir, wings := t.TempDir(), t.TempDir()
	cfg := pcfg.New()
	cfg.Panel.URL, cfg.Panel.Kind, cfg.Panel.EggID, cfg.Panel.WingsData = fakePanel(t, &env).URL, "pelican", 5, wings
	cfg.Defaults.HTTPURI = fakeBackend(t).URL
	a := &App{Dir: dir, Cfg: cfg, Secrets: pcfg.Secrets{}}

	device := filepath.Join(dir, pcfg.DeviceFile)
	creds := filepath.Join(wings, "s-1", "root", "_local", ".credentials.json")
	rootCreds := rootCredentials(filepath.Join(wings, "s-1")) // nevr-runtime 6.x's copy in the nEVR root
	for _, p := range []string{device, creds, rootCreds} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(`{"refresh_token":"r"}`), 0o600)
	}
	if !a.HasAccount() {
		t.Fatal("a device link counts as an account")
	}

	ctx := context.Background()
	if _, err := a.UsePassword(ctx, "123", "wrong", func(string) {}); err == nil {
		t.Fatal("wrong password accepted")
	}
	if a.Secrets[config.EnvPassword] != "" || !a.DeviceLinked() || env != nil {
		t.Fatal("a failed sign-in changed something")
	}

	var said []string
	if _, err := a.UsePassword(ctx, "123", "pw", func(s string) { said = append(said, s) }); err != nil {
		t.Fatal(err)
	}
	if a.DeviceLinked() {
		t.Error("device link kept")
	}
	if _, err := os.Stat(creds); !os.IsNotExist(err) {
		t.Error("server still has the device link")
	}
	if _, err := os.Stat(rootCreds); !os.IsNotExist(err) {
		t.Error("server's nEVR root still has the device link")
	}
	if env[config.EnvPassword] != "pw" || env[config.EnvDiscordID] != "123" || env["NEVR_REGIONS"] != "euw" {
		t.Errorf("server env %v", env)
	}
	saved, err := pcfg.LoadSecrets(dir)
	if err != nil || saved[config.EnvPassword] != "pw" {
		t.Errorf("secrets not saved: %v", err)
	}
	if len(said) != 2 {
		t.Errorf("progress %q", said)
	}
}

func TestRootCredentials(t *testing.T) {
	if got, want := rootCredentials("/v/s-1"), "/v/s-1/.wine/drive_c/users/container/AppData/Local/nevr/echovr/.credentials.json"; got != want {
		t.Errorf("got %s want %s", got, want)
	}
}
