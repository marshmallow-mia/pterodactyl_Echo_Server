// Package panelinstall installs Pterodactyl (stable or beta) or Pelican with
// Wings on this host, unattended. Each step first checks whether it is
// already done, so the installer can be re-run after a failure; with DryRun
// it only prints the commands and files, for people who prefer to run them
// by hand.
package panelinstall

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Step is one install step.
type Step struct {
	Name string
	// Done reports whether the step can be skipped. Nil means always run.
	Done func(ctx context.Context) bool
	Run  func(ctx context.Context, r *Runner) error
}

// Runner executes steps and records what it did.
type Runner struct {
	DryRun bool
	// Log receives progress lines.
	Log func(string)
	// Script collects every command and file of a dry run.
	Script strings.Builder
}

func (r *Runner) log(f string, a ...any) {
	if r.Log != nil {
		r.Log(fmt.Sprintf(f, a...))
	}
}

// Execute runs the steps in order.
func (r *Runner) Execute(ctx context.Context, steps []Step) error {
	for i, s := range steps {
		prefix := fmt.Sprintf("[%d/%d] %s", i+1, len(steps), s.Name)
		if !r.DryRun && s.Done != nil && s.Done(ctx) {
			r.log("%s: already done", prefix)
			continue
		}
		r.log("%s", prefix)
		if r.DryRun {
			fmt.Fprintf(&r.Script, "\n# --- %s\n", s.Name)
		}
		if err := s.Run(ctx, r); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
	}
	return nil
}

// Sh runs a bash script (set -euo pipefail), streaming its output.
func (r *Runner) Sh(ctx context.Context, script string) error {
	script = strings.TrimSpace(script)
	if r.DryRun {
		r.Script.WriteString(script + "\n")
		return nil
	}
	cmd := exec.CommandContext(ctx, "bash", "-c", "set -euo pipefail\n"+script)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "COMPOSER_ALLOW_SUPERUSER=1", "COMPOSER_NO_INTERACTION=1")
	return r.stream(cmd)
}

// Output runs a command and returns its trimmed stdout (not in dry runs).
func (r *Runner) Output(ctx context.Context, script string) (string, error) {
	if r.DryRun {
		r.Script.WriteString(script + "\n")
		return "", nil
	}
	out, err := exec.CommandContext(ctx, "bash", "-c", "set -euo pipefail\n"+script).Output()
	return strings.TrimSpace(string(out)), err
}

func (r *Runner) stream(cmd *exec.Cmd) error {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	var tail []string
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			if line == "" {
				continue
			}
			tail = append(tail, line)
			if len(tail) > 15 {
				tail = tail[1:]
			}
			r.log("    %s", line)
		}
		close(done)
	}()
	err := cmd.Run()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("%v\n%s", err, strings.Join(tail, "\n"))
	}
	return nil
}

// WriteFile writes a file (and its directory).
func (r *Runner) WriteFile(path, content string, mode os.FileMode) error {
	if r.DryRun {
		fmt.Fprintf(&r.Script, "install -D -m %o /dev/stdin %s <<'EOF'\n%sEOF\n", mode, path, ensureNL(content))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func ensureNL(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// Exists reports whether path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Has reports whether a command is on PATH.
func Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Active reports whether a systemd unit is active.
func Active(unit string) bool {
	return exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil
}

// shq quotes s for bash.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
