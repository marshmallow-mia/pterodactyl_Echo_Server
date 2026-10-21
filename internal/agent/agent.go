// Package agent is evr-ptero's background service: Prometheus metrics,
// Discord alerts and runtime updates for the Echo VR servers in the panel.
package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/instance"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/metrics"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/notify"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/status"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/update"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/updates"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// Agent watches one panel's Echo VR servers.
type Agent struct {
	Dir string
	Log func(string)

	mu      sync.Mutex
	samples []metrics.Sample
	watcher notify.Watcher
}

func (a *Agent) log(f string, args ...any) {
	if a.Log != nil {
		a.Log(fmt.Sprintf(f, args...))
	}
}

// Run blocks until ctx ends.
func (a *Agent) Run(ctx context.Context) error {
	app, err := core.Open(a.Dir)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	a.watcher.Host, a.watcher.Log = host, func(s string) { a.log("%s", s) }
	if l := app.Cfg.Metrics.Listen; l != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler(a.snapshot))
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
		srv := &http.Server{Addr: l, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			a.log("metrics on http://%s/metrics", l)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.log("metrics server: %v", err)
			}
		}()
		defer srv.Close()
	}
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	nextUpdate := time.Now().Add(2 * time.Minute)
	for {
		if app, err := core.Open(a.Dir); err != nil {
			a.log("config: %v", err)
		} else {
			a.watch(ctx, app)
			if time.Now().After(nextUpdate) {
				a.checkUpdates(ctx, app, host)
				nextUpdate = time.Now().Add(app.Cfg.Updates.Settings().Interval())
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (a *Agent) snapshot() []metrics.Sample {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]metrics.Sample(nil), a.samples...)
}

func notifier(app *core.App) *notify.Notifier {
	return &notify.Notifier{URL: app.Secrets[pcfg.SecretWebhook], Events: app.Cfg.Notify.Events}
}

func (a *Agent) watch(ctx context.Context, app *core.App) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	servers, err := app.Servers(cctx)
	if err != nil {
		a.log("listing servers: %v", err)
		return
	}
	var samples []metrics.Sample
	var obs []notify.Observation
	for _, s := range servers {
		st := status.Status{Instance: s.Name(), Port: s.Port}
		if s.Status != nil {
			st = *s.Status
			st.Instance = s.Name()
		}
		running := s.Power == "running" || s.Power == "starting"
		samples = append(samples, metrics.Sample{Status: st, Running: running})
		obs = append(obs, notify.Observation{Name: s.Name(), State: s.State(), Status: st, Running: running, Enabled: !s.Panel.Suspended && s.Power != "", Port: s.Port})
	}
	a.watcher.Notifier = notifier(app)
	a.watcher.Observe(ctx, obs)
	a.mu.Lock()
	a.samples = samples
	a.mu.Unlock()
}

func (a *Agent) checkUpdates(ctx context.Context, app *core.App, host string) {
	n := notifier(app)
	sched := &updates.Scheduler{Dir: app.Dir, Log: func(s string) { a.log("%s", s) }, Event: func(e updates.Event) {
		_ = n.Send(ctx, UpdateEvent(e, host))
	}}
	sched.Run(ctx, Components(app, func(s string) { a.log("%s", s) }), app.Cfg.Updates.Settings())
}

// Updater returns an updater for the node's shared runtime store whose
// canary is pinned through the panel.
func Updater(app *core.App, log func(string)) *update.Updater {
	return &update.Updater{Store: app.Store(), Fleet: fleet{dir: app.Dir}, Log: log}
}

type fleet struct{ dir string }

func (f fleet) Statuses(ctx context.Context) ([]status.Status, error) {
	app, err := core.Open(f.dir)
	if err != nil {
		return nil, err
	}
	servers, err := app.Servers(ctx)
	if err != nil {
		return nil, err
	}
	var out []status.Status
	for _, s := range servers {
		if s.Status == nil || s.Power != "running" {
			continue
		}
		st := *s.Status
		st.Instance = s.Name()
		if s.State() == "starting" {
			st.State = status.StateStarting
		}
		out = append(out, st)
	}
	return out, nil
}

func (f fleet) Pin(ctx context.Context, name, tag string) error {
	app, err := core.Open(f.dir)
	if err != nil {
		return err
	}
	s, err := app.Find(ctx, name)
	if err != nil {
		return err
	}
	if tag == "" {
		tag = "current"
	}
	return app.UpdateServer(ctx, s, func(set *instance.Settings) { set.Runtime = tag }, true)
}
