package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/notify"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// UnitPath is the agent's systemd unit.
const UnitPath = "/etc/systemd/system/evr-ptero-agent.service"

// ServiceInstalled reports whether the agent's systemd unit exists.
func ServiceInstalled() bool {
	_, err := os.Stat(UnitPath)
	return err == nil
}

// InstallService installs and (re)starts the agent as a systemd service.
func InstallService(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	unit := fmt.Sprintf("[Unit]\nDescription=evr-ptero agent (Echo VR server metrics, alerts, updates)\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nExecStart=%s --config-dir %s agent run\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\n", exe, dir)
	if err := os.WriteFile(UnitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "evr-ptero-agent.service"}, {"restart", "evr-ptero-agent.service"}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

// RemoveService stops the agent and removes its systemd unit.
func RemoveService() error {
	_ = exec.Command("systemctl", "disable", "--now", "evr-ptero-agent.service").Run()
	if err := os.Remove(UnitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

// SetWebhook stores the Discord webhook (empty turns alerts off) and sends a
// test message to it. A running agent reads it at its next check.
func SetWebhook(ctx context.Context, app *core.App, url string) error {
	if url == "" {
		delete(app.Secrets, pcfg.SecretWebhook)
		return app.Secrets.Save(app.Dir)
	}
	if err := TestWebhook(ctx, app, url); err != nil {
		return err
	}
	app.Secrets[pcfg.SecretWebhook] = url
	return app.Secrets.Save(app.Dir)
}

// TestWebhook sends a test message.
func TestWebhook(ctx context.Context, app *core.App, url string) error {
	host, _ := os.Hostname()
	return (&notify.Notifier{URL: url}).Send(ctx, notify.Event{Kind: "test", Level: notify.Info, Host: host,
		Title: "evr-ptero alerts are set up", Text: "This channel will get alerts about the Echo VR servers in " + app.Cfg.Panel.URL + "."})
}
