package config

import (
	"fmt"
	"runtime"
	"strings"
)

// ValidKey checks a key name: tab, escape, ctrl-space, alt-space, alt-up,
// alt-down, ctrl-<letter> or alt-<letter>. ctrl-c, ctrl-i (Tab), ctrl-j and
// ctrl-m (Enter) are taken by the terminal and the line editor; ctrl-x
// prefixes the bash integration's internal macros.
func ValidKey(name string) error {
	switch name {
	case "tab", "escape", "ctrl-space", "alt-space", "alt-up", "alt-down":
		return nil
	}
	if l, ok := letter(name, "ctrl-"); ok {
		if strings.ContainsRune("cijmx", l) {
			return fmt.Errorf("key %s is reserved", name)
		}
		return nil
	}
	if _, ok := letter(name, "alt-"); ok {
		return nil
	}
	return fmt.Errorf("unknown key %q (want tab, escape, ctrl-space, alt-space, alt-up, alt-down, ctrl-<letter> or alt-<letter>)", name)
}

func letter(name, prefix string) (rune, bool) {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || len(rest) != 1 || rest[0] < 'a' || rest[0] > 'z' {
		return 0, false
	}
	return rune(rest[0]), true
}

// Seqs returns what the shell binds for key name: zsh bindkey strings,
// readline key sequences (bash) or PSReadLine chords (pwsh). Empty or
// invalid names have none.
func Seqs(shell, name string) []string {
	if name == "" || ValidKey(name) != nil {
		return nil
	}
	if l, ok := letter(name, "ctrl-"); ok {
		switch shell {
		case "zsh":
			return []string{"^" + strings.ToUpper(string(l))}
		case "bash":
			return []string{`\C-` + string(l)}
		default:
			return []string{"Ctrl+" + string(l)}
		}
	}
	if l, ok := letter(name, "alt-"); ok {
		switch shell {
		case "zsh":
			return []string{"^[" + string(l)}
		case "bash":
			return []string{`\e` + string(l)}
		default:
			return []string{"Alt+" + string(l)}
		}
	}
	table := map[string][3][]string{
		"tab":       {{"^I"}, {`\C-i`}, {"Tab"}},
		"escape":    {{"^["}, {`\e`}, {"Escape"}},
		"alt-space": {{"^[ "}, {`\e `}, {"Alt+Spacebar"}},
		"alt-up":    {{"^[[1;3A", "^[^[[A"}, {`\e[1;3A`, `\e\e[A`}, {"Alt+UpArrow"}},
		"alt-down":  {{"^[[1;3B", "^[^[[B"}, {`\e[1;3B`, `\e\e[B`}, {"Alt+DownArrow"}},
	}
	if name == "ctrl-space" {
		// Unix terminals send Ctrl-Space as NUL, which PSReadLine names
		// Ctrl+@; the Windows console reports the real chord.
		pwsh := []string{"Ctrl+Spacebar"}
		if runtime.GOOS != "windows" {
			pwsh = append(pwsh, "Ctrl+@")
		}
		table[name] = [3][]string{{"^@"}, {`\C-@`}, pwsh}
	}
	row := table[name]
	switch shell {
	case "zsh":
		return row[0]
	case "bash":
		return row[1]
	default:
		return row[2]
	}
}
