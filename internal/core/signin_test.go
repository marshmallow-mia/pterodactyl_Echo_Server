package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

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
