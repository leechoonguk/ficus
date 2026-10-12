package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"ficus/internal/config"
	"ficus/internal/engine"
	"ficus/internal/termq"

	"golang.org/x/term"
)

// doctor prints what ficus sees of the environment, as "name: value" lines,
// and "! " lines for settings known to get in the way.
func doctor(out io.Writer) error {
	line := func(name, format string, a ...any) { fmt.Fprintf(out, "%s: %s\n", name, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { fmt.Fprintf(out, "! %s\n", fmt.Sprintf(format, a...)) }
	env := func(k string) string {
		if v, ok := os.LookupEnv(k); ok {
			return strconv.Quote(v)
		}
		return "(unset)"
	}

	s, path, err := config.Load()
	line("version", "%s", version)
	switch _, statErr := os.Stat(path); {
	case err != nil:
		line("config", "%s", path)
		warn("%v", err)
	case statErr != nil:
		line("config", "%s (not found)", path)
	default:
		line("config", "%s (ok)", path)
	}

	termName := os.Getenv("TERM")
	line("TERM", "%s", env("TERM"))
	colors := 256
	if runtime.GOOS == "windows" {
		// No terminfo: consoles with escape sequence support do 256 colors,
		// which is what the PowerShell integration assumes.
		line("colors", "256 (Windows console)")
	} else if out, err := exec.Command("tput", "colors").Output(); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && n > 0 {
			colors = n
		}
		line("colors", "%d", colors)
	} else {
		line("colors", "unknown (assuming 256)")
		warn("terminfo for %s missing — use kitten ssh / Ghostty ssh-terminfo", termName)
	}
	line("COLORTERM", "%s", env("COLORTERM"))
	line("NO_COLOR", "%s", env("NO_COLOR"))
	line("FORCE_COLOR", "%s", env("FORCE_COLOR"))
	name := initTheme(s.Theme)
	th, _ := engine.ResolveTheme(name, s.Color, colors, os.Getenv)
	line("theme", "%s (setting %s, color %s)", themeName(th), s.Theme, s.Color)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		switch light, ok := termq.Background(150 * time.Millisecond); {
		case !ok:
			line("background", "no answer")
		case light:
			line("background", "light")
		default:
			line("background", "dark")
		}
	}

	locale := "(unset)"
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(k); v != "" {
			locale = k + "=" + v
			break
		}
	}
	line("locale", "%s", locale)
	ascii := "off"
	if engine.ResolveGlyphs(s.ASCII, s.AmbiWidth, os.Getenv) == engine.GlyphsASCII {
		ascii = "on"
	}
	line("ascii", "%s (setting %s)", ascii, s.ASCII)
	line("ambiwidth", "%d", s.AmbiWidth)

	if os.Getenv("TMUX") != "" {
		if v, err := exec.Command("tmux", "-V").Output(); err == nil {
			line("tmux", "%s", strings.TrimSpace(string(v)))
		}
		if v, err := exec.Command("tmux", "show", "-gv", "escape-time").Output(); err == nil {
			ms := strings.TrimSpace(string(v))
			line("tmux escape-time", "%s ms", ms)
			if n, err := strconv.Atoi(ms); err == nil && n > 50 {
				warn("tmux escape-time %d ms delays Esc; use Ctrl-G or set escape-time 10", n)
			}
		}
	}
	ssh := "no"
	if os.Getenv("SSH_CONNECTION") != "" {
		ssh = "yes"
	}
	line("ssh", "%s", ssh)
	if os.Getenv("TERM_PROGRAM") == "vscode" {
		warn("VS Code: terminal IntelliSense and Ctrl+Space (triggerSuggest) conflict; disable terminal.integrated.suggest.enabled or rebind keys.pick")
	}
	if os.Getenv("TERMINAL_EMULATOR") == "JetBrains-JediTerm" {
		warn("JetBrains terminal has its own completion popup")
	}
	if os.Getenv("WT_SESSION") != "" {
		line("windows terminal", "yes")
	}

	cfg := engineConfig(s)
	if fileExists(cfg.Carapace) {
		line("carapace", "%s", cfg.Carapace)
	} else {
		line("carapace", "%s (missing)", cfg.Carapace)
		warn("ficus-carapace not found: only commands, files and variables complete")
	}
	entries, _ := os.ReadDir(cfg.CacheDir)
	line("cache", "%s (%d files)", cfg.CacheDir, len(entries))
	for _, action := range config.Actions {
		k := s.Keys[action]
		if k == "" {
			k = "(unbound)"
		}
		line("keys."+action, "%s", k)
	}
	return nil
}

func themeName(th engine.Theme) string {
	switch th {
	case engine.ThemeDark:
		return "dark"
	case engine.ThemeLight:
		return "light"
	case engine.ThemeANSI:
		return "ansi"
	}
	return "mono"
}
