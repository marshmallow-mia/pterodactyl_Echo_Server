package core

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// Where old installations put their cron jobs: root's crontab and this file.
var (
	etcCrontab = "/etc/crontab"
	crontabCmd = "crontab"
)

// CronLines lists the cron lines that refer to one of dirs.
func CronLines(dirs []string) []string {
	var out []string
	for _, text := range []string{rootCrontab(), readFile(etcCrontab)} {
		_, dropped := dropRefs(text, dirs)
		out = append(out, dropped...)
	}
	return out
}

// RemoveCronLines deletes the cron lines that refer to one of dirs and
// returns them.
func RemoveCronLines(dirs []string) ([]string, error) {
	var removed []string
	if kept, dropped := dropRefs(rootCrontab(), dirs); len(dropped) > 0 {
		cmd := exec.Command(crontabCmd, "-")
		cmd.Stdin = strings.NewReader(kept)
		if out, err := cmd.CombinedOutput(); err != nil {
			return removed, errors.New("crontab: " + strings.TrimSpace(string(out)))
		}
		removed = append(removed, dropped...)
	}
	if kept, dropped := dropRefs(readFile(etcCrontab), dirs); len(dropped) > 0 {
		fi, err := os.Stat(etcCrontab)
		if err != nil {
			return removed, err
		}
		if err := os.WriteFile(etcCrontab, []byte(kept), fi.Mode().Perm()); err != nil {
			return removed, err
		}
		removed = append(removed, dropped...)
	}
	return removed, nil
}

func rootCrontab() string {
	out, err := exec.Command(crontabCmd, "-l").Output()
	if err != nil {
		return "" // no crontab, or no cron
	}
	return string(out)
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// dropRefs splits text into the lines that do not refer to any of dirs and
// those that do (commented out or not).
func dropRefs(text string, dirs []string) (kept string, dropped []string) {
	if text == "" {
		return "", nil
	}
	var b strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		if refersTo(line, dirs) {
			dropped = append(dropped, strings.TrimRight(line, "\n"))
			continue
		}
		b.WriteString(line)
	}
	return b.String(), dropped
}

// refersTo reports whether line names one of dirs as a whole path
// (/opt/a matches /opt/a and /opt/a/x, not /opt/a_bu).
func refersTo(line string, dirs []string) bool {
	for _, d := range dirs {
		d = strings.TrimRight(d, "/")
		if d == "" {
			continue
		}
		rest := []byte(line)
		for {
			i := bytes.Index(rest, []byte(d))
			if i < 0 {
				break
			}
			end := i + len(d)
			if end == len(rest) || !isPathChar(rest[end]) || rest[end] == '/' {
				return true
			}
			rest = rest[end:]
		}
	}
	return false
}

func isPathChar(c byte) bool {
	return c == '/' || c == '_' || c == '-' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
