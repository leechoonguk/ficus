package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ficus/internal/config"
)

// Startup files are shared by every installed version of a shell (macOS
// /bin/bash is 3.2 next to a Homebrew bash 5). Loading the integration must
// never print anything or cut the rest of the startup file short on an
// unsupported version, and must load on a supported one.
func TestIntegrationLoadsOrStaysSilent(t *testing.T) {
	type shellCase struct {
		name, path, term string
	}
	var cases []shellCase
	seen := map[string]bool{}
	for _, name := range []string{"bash", "zsh"} {
		for _, p := range []string{"/bin/" + name, "/usr/bin/" + name} {
			if _, err := os.Stat(p); err == nil {
				cases = append(cases, shellCase{name: name, path: p})
			}
		}
		if p, err := exec.LookPath(name); err == nil {
			cases = append(cases, shellCase{name: name, path: p})
		}
	}
	if len(cases) == 0 {
		t.Skip("no bash or zsh installed")
	}

	var runs []shellCase
	for _, c := range cases {
		real, _ := filepath.EvalSymlinks(c.path)
		if seen[real] {
			continue
		}
		seen[real] = true
		// TERM=dumb (editor shells) cannot draw a menu: nothing loads.
		for _, term := range []string{"xterm-256color", "dumb"} {
			runs = append(runs, shellCase{c.name, c.path, term})
		}
	}
	for _, c := range runs {
		t.Run(c.path+" TERM="+c.term, func(t *testing.T) {
			script, err := Script(c.name, config.Defaults(), "ansi")
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			must(t, os.WriteFile(filepath.Join(dir, "ficus.sh"), []byte(script), 0o644))
			rc := `eval "$(cat ` + filepath.Join(dir, "ficus.sh") + `)"` + "\necho AFTER\n"

			var cmd *exec.Cmd
			var supported, probe string
			switch c.name {
			case "bash":
				must(t, os.WriteFile(filepath.Join(dir, "bashrc"), []byte(rc), 0o644))
				probe = "type -t __ficus_insert"
				cmd = exec.Command(c.path, "--noprofile", "--rcfile", filepath.Join(dir, "bashrc"), "-i", "-c", probe)
				supported = "(( BASH_VERSINFO[0] >= 4 ))"
			case "zsh":
				must(t, os.WriteFile(filepath.Join(dir, ".zshrc"), []byte(rc), 0o644))
				probe = "whence -w _ficus_accept"
				cmd = exec.Command(c.path, "-i", "-c", probe)
				cmd.Env = append(os.Environ(), "ZDOTDIR="+dir)
				supported = "autoload -Uz is-at-least; is-at-least 5.9"
			}
			cmd.Env = append(cmd.Environ(), "HOME="+dir, "FICUS_EXTRA_COMMANDS="+filepath.Join(dir, "cmds"), "TERM="+c.term)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, _ := cmd.Output()

			if !strings.Contains(string(out), "AFTER") {
				t.Errorf("the rest of the startup file did not run; stdout %q stderr %q", out, stderr.String())
			}
			isSupported := exec.Command(c.path, "-c", supported).Run() == nil
			dumb := c.term == "dumb"
			loaded := strings.Contains(string(out), "function")
			switch {
			case isSupported && dumb && loaded:
				t.Errorf("TERM=dumb cannot draw a menu, but the integration loaded")
			case isSupported && !dumb && !loaded:
				t.Errorf("supported shell did not load the integration; stdout %q stderr %q", out, stderr.String())
			case !isSupported && loaded:
				t.Errorf("unsupported shell loaded the integration")
			case !isSupported || dumb:
				if msg := withoutJobControlNotice(stderr.String()); msg != "" {
					t.Errorf("a shell that skips the integration printed %q", msg)
				}
			}
		})
	}
}

// An interactive shell without a terminal reports that job control is off;
// that line is the harness's, not ficus's.
func withoutJobControlNotice(s string) string {
	var keep []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" && !strings.Contains(line, "job control") && !strings.Contains(line, "작업 제어") &&
			!strings.Contains(strings.ToLower(line), "inappropriate ioctl") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
