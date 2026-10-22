package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marshmallow-mia/pterodactyl_Echo_Server/internal/pcfg"
)

// fakeCron points the cron helpers at files in a temp dir: root's crontab
// (through a fake crontab command) and /etc/crontab.
func fakeCron(t *testing.T, user, system string) (userFile, systemFile string) {
	t.Helper()
	dir := t.TempDir()
	userFile, systemFile = filepath.Join(dir, "root"), filepath.Join(dir, "crontab")
	os.WriteFile(userFile, []byte(user), 0o600)
	os.WriteFile(systemFile, []byte(system), 0o644)
	cmd := filepath.Join(dir, "crontab-cmd")
	script := "#!/bin/sh\nif [ \"$1\" = -l ]; then cat " + userFile + "; else cat > " + userFile + "; fi\n"
	if err := os.WriteFile(cmd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldCmd, oldEtc := crontabCmd, etcCrontab
	crontabCmd, etcCrontab = cmd, systemFile
	t.Cleanup(func() { crontabCmd, etcCrontab = oldCmd, oldEtc })
	return userFile, systemFile
}

func TestRefersTo(t *testing.T) {
	dirs := []string{"/opt/ready-at-dawn-echo-arena"}
	for line, want := range map[string]bool{
		"5 11 * * * find /opt/ready-at-dawn-echo-arena/logs/archive -delete": true,
		"* * * * * ls /opt/ready-at-dawn-echo-arena":                         true,
		"# * * * * * ls /opt/ready-at-dawn-echo-arena/":                      true,
		"* * * * * ls /opt/ready-at-dawn-echo-arena_bu/logs":                 false,
		"* * * * * ls /opt/ready-at-dawn-echo-arena2 /opt/other":             false,
	} {
		if got := refersTo(line, dirs); got != want {
			t.Errorf("%q: %v", line, got)
		}
	}
}

func TestRemoveCronLines(t *testing.T) {
	user, system := fakeCron(t,
		"MAILTO=root\n*/5 * * * * /usr/bin/bash /opt/pterodactyl_Echo_Server/scripts/move_logs.sh\n0 3 * * * /usr/local/bin/backup\n",
		"17 * * * * root cd / && run-parts --report /etc/cron.hourly\n5 11 * * * root find /opt/ready-at-dawn-echo-arena/logs/archive -type f -mtime +30 -delete\n")
	dirs := []string{"/opt/pterodactyl_Echo_Server/scripts", "/opt/ready-at-dawn-echo-arena"}
	if got := CronLines(dirs); len(got) != 2 {
		t.Fatalf("found %q", got)
	}
	removed, err := RemoveCronLines(dirs)
	if err != nil || len(removed) != 2 {
		t.Fatalf("removed %q, %v", removed, err)
	}
	u, _ := os.ReadFile(user)
	s, _ := os.ReadFile(system)
	if string(u) != "MAILTO=root\n0 3 * * * /usr/local/bin/backup\n" {
		t.Errorf("root crontab: %q", u)
	}
	if strings.Contains(string(s), "echo-arena") || !strings.Contains(string(s), "run-parts") {
		t.Errorf("/etc/crontab: %q", s)
	}
	if removed, _ := RemoveCronLines(dirs); len(removed) != 0 {
		t.Errorf("second run removed %q", removed)
	}
}

func TestCleanupLegacyRemovesCronWithScripts(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	repo := filepath.Join(root, "pterodactyl_Echo_Server")
	for _, p := range []string{"getBinaries.sh", "scripts/move_logs.sh"} {
		os.MkdirAll(filepath.Dir(filepath.Join(repo, p)), 0o755)
		os.WriteFile(filepath.Join(repo, p), nil, 0o644)
	}
	user, _ := fakeCron(t, "*/5 * * * * /usr/bin/bash "+repo+"/scripts/move_logs.sh\n", "")
	f := LegacyFiles{ScriptsDir: filepath.Join(repo, "scripts"), RepoDir: repo, RepoEntries: v1RepoEntries}
	a := &App{Cfg: &pcfg.Config{DataDir: data}, Secrets: pcfg.Secrets{}}
	if got := a.Removable(nil, f); len(got) != 1 || !strings.HasSuffix(got[0].What, "and its cron job") {
		t.Errorf("removable: %+v", got)
	}
	var said []string
	opts := MigrateOptions{Mode: MigrateRecreate, Remove: []string{RemoveScripts}}
	if err := a.cleanupLegacy(t.Context(), nil, f, opts, func(s string) { said = append(said, s) }); err != nil {
		t.Fatal(err)
	}
	if u, _ := os.ReadFile(user); len(u) != 0 {
		t.Errorf("cron job left: %q", u)
	}
	if !slices.ContainsFunc(said, func(s string) bool { return strings.HasPrefix(s, "removed the old cron job") }) {
		t.Errorf("not reported: %q", said)
	}
}
