package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/game"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/ghrel"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/notify"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/selfupdate"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/update"
	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/updates"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panelinstall"
)

// Components are the parts evr-ptero updates: itself, the node's shared game
// files, nevr-runtime, and the panel and Wings when they run on this host.
func Components(app *core.App, log func(string)) []updates.Component {
	comps := []updates.Component{
		&updates.Self{U: &selfupdate.Updater{Repo: core.Repo, Binary: "evr-ptero", Current: core.Version}, After: PostUpdate(app.Dir)},
	}
	gameDir := filepath.Join(app.Cfg.DataDir, "game")
	if _, err := os.Stat(gameDir); err == nil || app.Cfg.GameSource == "mount" {
		comps = append(comps, &updates.Game{In: game.Installer{Dir: gameDir}})
	}
	comps = append(comps, &updates.Runtime{U: Updater(app, log), Policy: func() update.Policy {
		return update.Policy{Channel: app.Cfg.Runtime.Channel, Pinned: app.Cfg.Runtime.Pinned}
	}})
	if inst, err := panelinstall.DetectInstalled(); err == nil {
		if inst.Dir != "" && inst.Panel != "" {
			comps = append(comps, &Panel{app: app, in: inst})
		}
		if inst.Wings != "" {
			comps = append(comps, &Wings{in: inst})
		}
	}
	return comps
}

// UpdateEvent turns a scheduler event into a Discord alert.
func UpdateEvent(e updates.Event, host string) notify.Event {
	title, st := e.Component.Title(), e.Status
	switch e.Kind {
	case updates.EventApplied:
		return notify.Event{Kind: notify.EventUpdateApplied, Level: notify.Good, Host: host, Title: title + " updated to " + st.Latest,
			Text: "Installed automatically (was " + st.Installed + ")."}
	case updates.EventFailed:
		return notify.Event{Kind: notify.EventUpdateRolledBack, Level: notify.Warning, Host: host, Title: title + " " + st.Latest + " was not installed",
			Text: e.Err.Error() + "\nStaying on " + st.Installed + "."}
	}
	return notify.Event{Kind: notify.EventUpdateAvailable, Level: notify.Info, Host: host, Title: title + " " + st.Latest + " is available",
		Text: "Installed: " + st.Installed + ". Install it in evr-ptero (U: updates) or with `evr-ptero update apply " + e.Component.Name() + "`."}
}

// PostUpdate runs the new binary's post-update step (the panel setup, which
// moves the egg and servers to the new image) and restarts the agent.
func PostUpdate(dir string) func(ctx context.Context, say func(string)) error {
	return func(ctx context.Context, say func(string)) error {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return err
		}
		out, err := exec.CommandContext(ctx, exe, "--config-dir", dir, "post-update").CombinedOutput()
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l != "" {
				say(l)
			}
		}
		if err != nil {
			return fmt.Errorf("post-update: %w", err)
		}
		if ServiceInstalled() {
			// --no-block: when the agent itself runs this, it is the one restarted.
			_ = exec.Command("systemctl", "restart", "--no-block", "evr-ptero-agent.service").Run()
			say("background service restarting with the new version")
		}
		return nil
	}
}

// DryRunner is implemented by parts that can print what Apply would run.
type DryRunner interface {
	DryRun(ctx context.Context, version string) (string, error)
}

// Panel updates the panel on this host.
type Panel struct {
	app *core.App
	in  panelinstall.Installed
}

func (p *Panel) Name() string { return "panel" }
func (p *Panel) Title() string {
	if p.in.Product == "pelican" {
		return "Pelican panel"
	}
	return "Pterodactyl panel"
}

func (p *Panel) releases(ctx context.Context) ([]ghrel.Release, error) {
	return panelinstall.PanelReleases(ctx, &ghrel.Client{}, p.in)
}

func (p *Panel) Check(ctx context.Context) (updates.Status, error) {
	st := updates.Status{Installed: p.in.Panel}
	rels, err := p.releases(ctx)
	if err != nil {
		return st, err
	}
	if r, ok := p.target(rels, ""); ok {
		st.Latest, st.Available = r.Tag, selfupdate.Newer(p.in.Panel, r.Tag)
	}
	return st, nil
}

// target is version, or the newest release of the update channel.
func (p *Panel) target(rels []ghrel.Release, version string) (ghrel.Release, bool) {
	for _, r := range rels {
		if version != "" && (r.Tag == version || r.Tag == "v"+version) {
			return r, true
		}
		if version == "" && p.in.InChannel(r, p.app.Cfg.Updates.PanelChannel) {
			return r, true
		}
	}
	return ghrel.Release{}, false
}

func (p *Panel) Versions(ctx context.Context) ([]string, error) {
	rels, err := p.releases(ctx)
	var out []string
	for _, r := range rels {
		v := r.Tag
		if !p.in.InChannel(r, "") {
			v += " (other channel)"
		}
		out = append(out, v)
	}
	return out, err
}

// SwitchesChannel reports whether version leaves the installed channel, e.g.
// Pterodactyl 1.x to 2.x: a one-way migration.
func (p *Panel) SwitchesChannel(ctx context.Context, version string) bool {
	rels, err := p.releases(ctx)
	if err != nil {
		return false
	}
	r, ok := p.target(rels, strings.TrimSuffix(version, " (other channel)"))
	return ok && !p.in.InChannel(r, "")
}

func (p *Panel) Apply(ctx context.Context, version string, say func(string)) error {
	return p.apply(ctx, strings.TrimSuffix(version, " (other channel)"), &panelinstall.Runner{Log: say}, say)
}

func (p *Panel) DryRun(ctx context.Context, version string) (string, error) {
	r := &panelinstall.Runner{DryRun: true}
	err := p.apply(ctx, version, r, func(string) {})
	return r.Script.String(), err
}

func (p *Panel) apply(ctx context.Context, version string, r *panelinstall.Runner, say func(string)) error {
	rels, err := p.releases(ctx)
	if err != nil {
		return err
	}
	rel, ok := p.target(rels, version)
	if !ok {
		if version == "" {
			return errors.New("no newer panel release in the update channel")
		}
		return fmt.Errorf("panel release %s not found (or older than the installed %s)", version, p.in.Panel)
	}
	c := &ghrel.Client{}
	backup, err := panelinstall.UpgradePanel(ctx, p.in, rel, c, r)
	if r.DryRun {
		return err
	}
	if err == nil {
		say("checking the panel API")
		err = p.waitAPI(ctx)
	}
	if err != nil {
		say("the update failed (" + err.Error() + "); restoring the backup")
		if rerr := panelinstall.RestorePanel(ctx, backup, r); rerr != nil {
			return fmt.Errorf("%v; restoring the backup failed too: %v (backup in %s)", err, rerr, backup.Dir)
		}
		return fmt.Errorf("%v; the panel was restored to %s", err, p.in.Panel)
	}
	if !p.in.InChannel(rel, "") {
		say("new channel: preparing the panel for Echo VR again")
		if rel.Version.Major >= 2 && p.in.Product == "pterodactyl" {
			panelinstall.SetProduct("pterodactyl-beta")
		}
		if _, err := core.Setup(ctx, p.app.Dir, core.SetupOptions{}, func(step, d string) { say(step + ": " + d) }); err != nil {
			return err
		}
	}
	say(fmt.Sprintf("panel %s installed; backup of %s in %s", rel.Tag, p.in.Panel, backup.Dir))
	return nil
}

func (p *Panel) waitAPI(ctx context.Context) error {
	var err error
	for i := 0; i < 20; i++ {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err = p.app.Panel().Users(cctx)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("the panel API does not answer: %w", err)
}

// Wings updates Wings on this host.
type Wings struct{ in panelinstall.Installed }

func (w *Wings) Name() string  { return "wings" }
func (w *Wings) Title() string { return "Wings" }

func (w *Wings) Check(ctx context.Context) (updates.Status, error) {
	st := updates.Status{Installed: w.in.Wings}
	rels, err := panelinstall.WingsReleases(ctx, &ghrel.Client{}, w.in)
	if err != nil || len(rels) == 0 {
		return st, err
	}
	st.Latest, st.Available = rels[0].Tag, selfupdate.Newer(w.in.Wings, rels[0].Tag)
	return st, nil
}

func (w *Wings) Versions(ctx context.Context) ([]string, error) {
	rels, err := panelinstall.WingsReleases(ctx, &ghrel.Client{}, w.in)
	var out []string
	for _, r := range rels {
		out = append(out, r.Tag)
	}
	return out, err
}

func (w *Wings) Apply(ctx context.Context, version string, say func(string)) error {
	return w.apply(ctx, version, &panelinstall.Runner{Log: say})
}

func (w *Wings) DryRun(ctx context.Context, version string) (string, error) {
	r := &panelinstall.Runner{DryRun: true}
	err := w.apply(ctx, version, r)
	return r.Script.String(), err
}

func (w *Wings) apply(ctx context.Context, version string, r *panelinstall.Runner) error {
	c := &ghrel.Client{}
	rels, err := panelinstall.WingsReleases(ctx, c, w.in)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		if version == "" || rel.Tag == version || rel.Tag == "v"+version {
			return panelinstall.UpgradeWings(ctx, rel, c, r)
		}
	}
	return fmt.Errorf("Wings release %s not found (or older than the installed %s)", version, w.in.Wings)
}
