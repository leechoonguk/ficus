// Package shell holds the shell integration scripts printed by `ficus init`.
package shell

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"ficus/internal/config"
)

//go:embed ficus.*
var scripts embed.FS

var files = map[string]string{
	"zsh":  "ficus.zsh",
	"bash": "ficus.bash",
	"pwsh": "ficus.ps1",
}

// Script returns the integration for shell with @FICUS@ replaced by the
// quoted path of the running ficus binary, so the integration keeps working
// when ficus is not on PATH (e.g. on a remote host), and @CONFIG@ by the
// settings the shell side needs. theme is the resolved theme name.
func Script(shell string, s config.Settings, theme string) (string, error) {
	name, ok := files[shell]
	if !ok {
		return "", fmt.Errorf("no integration for %q", shell)
	}
	data, err := scripts.ReadFile(name)
	if err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "ficus"
	}
	quote := quotePosix
	if shell == "pwsh" {
		quote = quotePwsh
	}
	out := strings.ReplaceAll(string(data), "@FICUS@", quote(exe))
	return strings.ReplaceAll(out, "@CONFIG@", settings(shell, s, theme)), nil
}

// settings renders the assignments that replace @CONFIG@.
func settings(shell string, s config.Settings, theme string) string {
	var b strings.Builder
	if shell == "pwsh" {
		fmt.Fprintf(&b, "$__ficus.Up = %s\n", quotePwsh(s.UpArrow))
		fmt.Fprintf(&b, "$__ficus.Auto = %s\n", pwshBool(s.AutoPopup))
		fmt.Fprintf(&b, "$__ficus.Ghost = %s\n", pwshBool(s.Ghost))
		fmt.Fprintf(&b, "$__ficus.Theme = %s\n", quotePwsh(theme))
		fmt.Fprintf(&b, "$__ficus.Budget = %s", quotePwsh(s.Budget.String()))
		for _, action := range config.Actions {
			var q []string
			for _, seq := range config.Seqs(shell, s.Keys[action]) {
				q = append(q, quotePwsh(seq))
			}
			fmt.Fprintf(&b, "\n$__ficus.Key%s = [string[]]@(%s)", strings.ToUpper(action[:1])+action[1:], strings.Join(q, ", "))
		}
		return b.String()
	}
	global := "typeset -g "
	array := "typeset -ga "
	if shell == "bash" {
		global, array = "", ""
	}
	fmt.Fprintf(&b, "%s__ficus_cfg_up=%s\n", global, quotePosix(s.UpArrow))
	fmt.Fprintf(&b, "%s__ficus_cfg_auto=%d __ficus_cfg_ghost=%d\n", global, bit(s.AutoPopup), bit(s.Ghost))
	fmt.Fprintf(&b, "%s__ficus_theme=%s __ficus_budget=%s", global, quotePosix(theme), quotePosix(s.Budget.String()))
	for _, action := range config.Actions {
		var q []string
		for _, seq := range config.Seqs(shell, s.Keys[action]) {
			q = append(q, quotePosix(seq))
		}
		fmt.Fprintf(&b, "\n%s__ficus_key_%s=(%s)", array, action, strings.Join(q, " "))
	}
	return b.String()
}

func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}

func pwshBool(v bool) string {
	if v {
		return "$true"
	}
	return "$false"
}

func quotePosix(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func quotePwsh(s string) string  { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
