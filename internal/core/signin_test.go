package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/nevr/config"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

func setDeviceLink(t *testing.T, flag bool, env string) {
	t.Helper()
	old := DeviceLinkFlow
	DeviceLinkFlow = flag
	t.Setenv(DeviceLinkEnv, env)
	t.Cleanup(func() { DeviceLinkFlow = old })
}

func TestDeviceLinkEnabled(t *testing.T) {
	for _, c := range []struct {
		flag bool
		env  string
		want bool
	}{
		{false, "", false},
		{false, "0", false},
		{false, "maybe", false},
		{true, "", true},
		{false, "1", true},
		{false, "true", true},
	} {
		setDeviceLink(t, c.flag, c.env)
		if got := DeviceLinkEnabled(); got != c.want {
			t.Errorf("flag %v env %q: got %v, want %v", c.flag, c.env, got, c.want)
		}
	}
}

func TestLinkDeviceOff(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := &pcfg.Config{}
	cfg.Defaults.HTTPURI = srv.URL
	a := &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{}}

	setDeviceLink(t, false, "")
	_, err := a.UseDeviceLink(context.Background(), nil, func(string) {})
	if !errors.Is(err, ErrDeviceLinkOff) || hits.Load() != 0 {
		t.Fatalf("without the switch: got %v after %d request(s), want ErrDeviceLinkOff and none", err, hits.Load())
	}

	setDeviceLink(t, false, "1")
	if _, err := a.UseDeviceLink(context.Background(), nil, func(string) {}); errors.Is(err, ErrDeviceLinkOff) || hits.Load() == 0 {
		t.Fatalf("with %s=1 the link must start: got %v after %d request(s)", DeviceLinkEnv, err, hits.Load())
	}
}

// A password needs the Discord ID; without a password nothing is missing:
// each server signs in by its console code (nevr-runtime v6.9.0+).
func TestSecretsMissing(t *testing.T) {
	for _, c := range []struct {
		sec  pcfg.Secrets
		want int
	}{
		{pcfg.Secrets{}, 0},
		{pcfg.Secrets{config.EnvDiscordID: "1"}, 0},
		{pcfg.Secrets{config.EnvPassword: "x"}, 1},
		{pcfg.Secrets{config.EnvPassword: "x", config.EnvDiscordID: "1"}, 0},
	} {
		if got := c.sec.Missing(); len(got) != c.want {
			t.Errorf("%v: missing %v", c.sec, got)
		}
	}
}

// Without a password there is nothing to check from the host.
func TestLoginWithTheConsoleCode(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := &pcfg.Config{}
	cfg.Defaults.HTTPURI = srv.URL
	a := &App{Dir: t.TempDir(), Cfg: cfg, Secrets: pcfg.Secrets{}}
	if !a.HasAccount() || !a.SignsInByCode() {
		t.Errorf("HasAccount %v SignsInByCode %v", a.HasAccount(), a.SignsInByCode())
	}
	if _, err := a.Login(context.Background()); !errors.Is(err, ErrSignInByCode) || hits.Load() != 0 {
		t.Errorf("Login: %v after %d request(s)", err, hits.Load())
	}
	a.Secrets[config.EnvPassword] = "x"
	if a.SignsInByCode() || a.HasAccount() {
		t.Error("a password without a Discord ID")
	}
	// A host-wide device link (5.x) is not the console code.
	delete(a.Secrets, config.EnvPassword)
	os.WriteFile(filepath.Join(a.Dir, pcfg.DeviceFile), []byte(`{"refresh_token":"r"}`), 0o600)
	if a.SignsInByCode() {
		t.Error("a device-linked host counts as the console code")
	}
}

// UseSignInCode drops the password from the secrets and the servers, and the
// sign-in a password left in a server's root; one a code made is kept.
func TestUseSignInCode(t *testing.T) {
	var env map[string]any
	dir, wings := t.TempDir(), t.TempDir()
	cfg := pcfg.New()
	cfg.Panel.URL, cfg.Panel.Kind, cfg.Panel.EggID, cfg.Panel.WingsData = fakePanel(t, &env).URL, "pelican", 5, wings
	a := &App{Dir: dir, Cfg: cfg, Secrets: pcfg.Secrets{config.EnvDiscordID: "123", config.EnvPassword: "pw", config.EnvHTTPKey: "k"}}
	legacy := filepath.Join(wings, "s-1", "root", "_local", ".credentials.json")
	root := rootCredentials(filepath.Join(wings, "s-1"))
	for _, p := range []string{legacy, root} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(`{"refresh_token":"r"}`), 0o600)
	}
	if err := a.UseSignInCode(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if env[config.EnvPassword] != "" || env[config.EnvDiscordID] != "" || env[config.EnvHTTPKey] != "k" {
		t.Errorf("server env %v", env)
	}
	saved, _ := pcfg.LoadSecrets(dir)
	if saved[config.EnvPassword] != "" || saved[config.EnvDiscordID] != "" || saved[config.EnvHTTPKey] != "k" {
		t.Errorf("secrets %v", saved)
	}
	for _, p := range []string{legacy, root} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s kept after the password", p)
		}
	}
	// Already on the code: the server's approved sign-in stays; an old 5.x
	// link in _local goes.
	os.WriteFile(root, []byte(`{"refresh_token":"approved"}`), 0o600)
	os.WriteFile(legacy, []byte(`{"refresh_token":"old"}`), 0o600)
	if err := a.UseSignInCode(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(root); string(b) != `{"refresh_token":"approved"}` {
		t.Errorf("approved sign-in removed: %q", b)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("old _local link kept")
	}
}
