package core

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

func writeLocal(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "_local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_local", name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyCredentialsConfigJSON(t *testing.T) {
	dir := t.TempDir()
	writeLocal(t, dir, "config.json", `{
  "loginservice_host": "ws://g.echovrce.com:80/login?auth=x&discordid=123456789&password=hunter22",
  "serverdb_host": "ws://g.echovrce.com:80/serverdb?discordid=123456789&password=hunter22&regions=euw"
}`)
	id, pw, key := LegacyCredentials(dir)
	if id != "123456789" || pw != "hunter22" || key != "" {
		t.Fatalf("got %q %q %q", id, pw, key)
	}
}

func TestLegacyCredentialsConfigYAML(t *testing.T) {
	dir := t.TempDir()
	writeLocal(t, dir, "config.yaml", "identity:\n  discord_id: \"42\"\nauth:\n  password: pw\n  http_key: k\n")
	id, pw, key := LegacyCredentials(dir)
	if id != "42" || pw != "pw" || key != "k" {
		t.Fatalf("got %q %q %q", id, pw, key)
	}
}

func TestLegacyCredentialsMissing(t *testing.T) {
	if id, pw, _ := LegacyCredentials(t.TempDir()); id != "" || pw != "" {
		t.Fatalf("got %q %q", id, pw)
	}
}

func TestLegacyDirs(t *testing.T) {
	got := legacyDirs([]string{LegacyGameDir, "/srv/echo"}, "/var/lib/pterodactyl/volumes", []string{"u1", ""})
	want := []string{LegacyGameDir, "/srv/echo", "/var/lib/pterodactyl/volumes/u1", "/var/lib/pterodactyl/volumes/u1/ready-at-dawn-echo-arena"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestMigrateOptionsValidate(t *testing.T) {
	if err := (MigrateOptions{}).Validate(); err == nil {
		t.Error("no mode accepted")
	}
	if err := (MigrateOptions{Mode: MigrateKeep, Logs: LogsKeep, Remove: []string{RemoveGame}}).Validate(); err == nil {
		t.Error("keeping logs inside a deleted game folder was accepted")
	}
	if err := (MigrateOptions{Mode: MigrateRecreate, Remove: []string{"image"}}).Validate(); err == nil {
		t.Error("unknown removal accepted")
	}
	if err := (MigrateOptions{Mode: MigrateRecreate, Logs: LogsDelete, Remove: []string{RemoveGame, RemoveScripts, RemovePanel}}).Validate(); err != nil {
		t.Error(err)
	}
}

func TestCleanupLegacy(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	gameDir, repo := filepath.Join(root, "ready-at-dawn-echo-arena"), filepath.Join(root, "pterodactyl_Echo_Server")
	for _, p := range []string{
		filepath.Join(gameDir, "logs/de5935ba-759f/a.log"), filepath.Join(gameDir, "logs/de5935ba-759f/old/b.log"),
		filepath.Join(repo, "getBinaries.sh"), filepath.Join(repo, "scripts/start-echo.sh"), filepath.Join(repo, "notes.txt"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{Cfg: &pcfg.Config{DataDir: data}, Secrets: pcfg.Secrets{}}
	f := LegacyFiles{GameDir: gameDir, ScriptsDir: filepath.Join(repo, "scripts"), RepoDir: repo, RepoEntries: v1RepoEntries}

	// Defaults: logs move, nothing else is touched.
	if err := a.cleanupLegacy(context.Background(), nil, f, MigrateOptions{Mode: MigrateRecreate}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "logs", "v1", "de5935ba-759f", "a.log")); err != nil {
		t.Errorf("logs not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "getBinaries.sh")); err != nil {
		t.Error("scripts deleted without being asked")
	}

	opts := MigrateOptions{Mode: MigrateRecreate, Remove: []string{RemoveGame, RemoveScripts}}
	if err := a.cleanupLegacy(context.Background(), nil, f, opts, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gameDir); !os.IsNotExist(err) {
		t.Error("old game folder left")
	}
	left, _ := os.ReadDir(repo)
	if len(left) != 1 || left[0].Name() != "notes.txt" {
		t.Errorf("repo left %v, want only notes.txt", left)
	}
}
