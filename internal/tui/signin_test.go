package tui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/marshmallow-mia/Echo-VR-Server-on-Docker/v2/pkg/status"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/panel"
	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

func setDeviceLink(t *testing.T, on bool) {
	t.Helper()
	old := core.DeviceLinkFlow
	core.DeviceLinkFlow = on
	t.Setenv(core.DeviceLinkEnv, "")
	t.Cleanup(func() { core.DeviceLinkFlow = old })
}

// accountPage shows the first sign-in page of the wizard's account groups.
func accountPage(t *testing.T) (string, *account) {
	t.Helper()
	a := &account{}
	f := huh.NewForm(append([]*huh.Group{huh.NewGroup(huh.NewNote().Title("Welcome"))}, accountGroups(a)...)...)
	f = pump(f, f.Init())
	f = pump(f, func() tea.Msg { return tea.KeyPressMsg{Code: tea.KeyEnter} })
	return f.View(), a
}

// pump feeds cmd's messages to the form until it is quiet. Commands that do
// not answer at once (cursor blinks, ticks) are dropped.
func pump(f *huh.Form, cmd tea.Cmd) *huh.Form {
	queue := run(cmd)
	for i := 0; i < 100 && len(queue) > 0; i++ {
		var msg tea.Msg
		var model huh.Model
		msg, queue = queue[0], queue[1:]
		model, cmd = f.Update(msg)
		f = model.(*huh.Form)
		queue = append(queue, run(cmd)...)
	}
	return f
}

func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(20 * time.Millisecond):
		return nil
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, run(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// The wizard's sign-in defaults to each server's console code; the password
// is the second choice; the host-wide device link only with --device-link.
func TestWizardDefaultsToTheConsoleCode(t *testing.T) {
	setDeviceLink(t, false)
	v, a := accountPage(t)
	code, pw := strings.Index(v, "Sign-in code on each server's console"), strings.Index(v, "Discord ID and server password")
	if code < 0 || pw < 0 || code > pw {
		t.Fatalf("the console code must come first, the password second:\n%s", v)
	}
	if !regexp.MustCompile(`> Sign-in code on each server's console`).MatchString(ansi.Strip(v)) || a.signIn != "code" {
		t.Errorf("the console code is not preselected (%q):\n%s", a.signIn, v)
	}
	if strings.Contains(v, "Device link") || strings.Contains(v, "Server password") {
		t.Fatalf("without --device-link: no device link, and no password field before it is chosen:\n%s", v)
	}
	if id, pw := (&account{signIn: "code", discordID: "1", password: "x"}).fields(); id != "" || pw != "" {
		t.Errorf("console code keeps %q %q", id, pw)
	}
}

func TestWizardOffersTheDeviceLinkWithTheSwitch(t *testing.T) {
	setDeviceLink(t, true)
	if v, _ := accountPage(t); !strings.Contains(v, "Device link") {
		t.Fatalf("with --device-link the wizard offers the device link too:\n%s", v)
	}
}

func TestNoLinkAfterTheWizardWithoutTheSwitch(t *testing.T) {
	setDeviceLink(t, false)
	// signIn "device" can only be picked with the switch; without it nothing links.
	if err := linkIfNoPassword(context.Background(), nil, &account{signIn: "device"}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	// The console code never links, even with the switch.
	setDeviceLink(t, true)
	if err := linkIfNoPassword(context.Background(), nil, &account{signIn: "code"}, func(string) {}); err != nil {
		t.Fatal(err)
	}
}

func TestAccountScreenWithTheConsoleCode(t *testing.T) {
	setDeviceLink(t, false)
	m := New(context.Background(), t.TempDir())
	m.account = &accountInfo{err: core.ErrSignInByCode}
	v := m.renderAccount()
	if !strings.Contains(v, "echovrce.com/device") || !strings.Contains(v, "to sign in with your Discord ID and server password instead") {
		t.Errorf("console code screen:\n%s", v)
	}
	if strings.Contains(v, "link this host") {
		t.Errorf("no device link without the switch:\n%s", v)
	}
}

func TestAccountScreenIgnoresLWithoutTheSwitch(t *testing.T) {
	setDeviceLink(t, false)
	m := New(context.Background(), t.TempDir())
	m.screen = scrAccount
	if _, cmd := m.updateAccount(tea.KeyPressMsg{Code: 'l', Text: "l"}); cmd != nil {
		t.Fatal("l must do nothing without --device-link")
	}
	if m.busyTitle != "" {
		t.Fatalf("a task started: %q", m.busyTitle)
	}
}

func TestMigrateAccountOptions(t *testing.T) {
	for _, on := range []bool{false, true} {
		setDeviceLink(t, on)
		opts, def := migrateAccountOptions(core.LegacyAccount{}, false)
		if def != "code" || opts[0].Value != "code" || opts[1].Value != "password" {
			t.Errorf("device link %v: default %q, options %v; want the console code first", on, def, opts)
		}
		hasLink := false
		for _, o := range opts {
			hasLink = hasLink || o.Value == "link"
		}
		if hasLink != on {
			t.Errorf("device link %v: link offered = %v", on, hasLink)
		}
		if _, def := migrateAccountOptions(core.LegacyAccount{DiscordID: "1"}, true); def != "legacy" {
			t.Errorf("device link %v: with an old account the default is %q, want legacy", on, def)
		}
	}
}

// A server waiting for its code shows the whole link on the dashboard.
func TestDashboardShowsTheApprovalLink(t *testing.T) {
	m := New(context.Background(), t.TempDir())
	m.servers = []core.Server{{Panel: panel.Server{Name: "Echo VR 1", Identifier: "abc"}, Port: 6792, Power: "running",
		Status: &status.Status{State: status.StateAwaitingApproval, UpdatedAt: time.Now(),
			SignInURL: "https://echovrce.com/device?code=TEST", SignInExpires: time.Now().Add(5 * time.Minute)}}}
	m.refresh()
	row := strings.Join(m.table.Rows()[0], " ")
	if !strings.Contains(row, "approve code") || !strings.Contains(row, "https://echovrce.com/device?code=TEST") {
		t.Errorf("row: %s", row)
	}
	if d := m.approvalDetail(); !strings.Contains(d, "https://echovrce.com/device?code=TEST") || !strings.Contains(d, "host account") {
		t.Errorf("detail: %q", d)
	}
	m.App = &core.App{Cfg: pcfg.New(), Secrets: pcfg.Secrets{}}
	if v := ansi.Strip(m.render()); !strings.Contains(v, "waits for its sign-in") || !strings.Contains(v, "https://echovrce.com/device?code=TEST") {
		t.Errorf("dashboard:\n%s", v)
	}
}
