package core

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
