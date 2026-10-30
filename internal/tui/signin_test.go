package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/core"
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

func TestWizardAsksForThePassword(t *testing.T) {
	setDeviceLink(t, false)
	v, a := accountPage(t)
	if !strings.Contains(v, "Server password") || strings.Contains(v, "echovrce.com/device") {
		t.Fatalf("without --device-link the wizard must ask for the password, not offer a device link:\n%s", v)
	}
	if a.signIn != "password" {
		t.Fatalf("sign-in %q, want password", a.signIn)
	}
}

func TestWizardOffersTheDeviceLinkWithTheSwitch(t *testing.T) {
	setDeviceLink(t, true)
	v, _ := accountPage(t)
	if !strings.Contains(v, "echovrce.com/device") || !strings.Contains(v, "server password (needed for nevr-runtime 6.x)") {
		t.Fatalf("with --device-link the wizard offers both, the password first:\n%s", v)
	}
	if strings.Index(v, "server password (needed") > strings.Index(v, "Device link at") {
		t.Fatalf("the password must come first:\n%s", v)
	}
}

func TestNoLinkAfterTheWizardWithoutTheSwitch(t *testing.T) {
	setDeviceLink(t, false)
	// signIn "device" can only be picked with the switch; without it nothing links.
	if err := linkIfNoPassword(context.Background(), nil, &account{signIn: "device"}, func(string) {}); err != nil {
		t.Fatal(err)
	}
}

func TestAccountScreenWithoutSignIn(t *testing.T) {
	for _, on := range []bool{false, true} {
		setDeviceLink(t, on)
		m := New(context.Background(), t.TempDir())
		m.account = &accountInfo{err: core.ErrNoAccount}
		v := m.renderAccount()
		if !strings.Contains(v, "to sign in with your Discord ID and server password") {
			t.Errorf("device link %v: no password sign-in offered:\n%s", on, v)
		}
		if got := strings.Contains(v, "echovrce.com/device"); got != on {
			t.Errorf("device link %v: link offered = %v:\n%s", on, got, v)
		}
		if got := strings.Contains(signInFirst(), "l links"); got != on {
			t.Errorf("device link %v: flash offers l = %v", on, got)
		}
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
		if def != "password" {
			t.Errorf("device link %v: default %q, want password", on, def)
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
