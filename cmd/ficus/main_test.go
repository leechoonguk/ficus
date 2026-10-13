package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolate gives run() a PATH of fake commands, an empty cache and no user
// config, and returns the directory holding them.
func isolate(t *testing.T, commands ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake commands are shell scripts")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range commands {
		if err := os.WriteFile(filepath.Join(bin, c), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("FICUS_CACHE_DIR", filepath.Join(dir, "cache"))
	t.Setenv("FICUS_CARAPACE", filepath.Join(dir, "no-carapace"))
	t.Setenv("FICUS_CONFIG", filepath.Join(dir, "none.toml"))
	t.Setenv("FICUS_EXTRA_COMMANDS", "")
	for _, k := range []string{"FICUS_MIN_INPUT", "FICUS_GROUPS", "FICUS_RECENT", "FICUS_GHOST", "FICUS_ROWS", "NO_COLOR"} {
		t.Setenv(k, "")
	}
	return dir
}

func runOut(t *testing.T, args ...string) string {
	t.Helper()
	var b strings.Builder
	if err := run(args, &b); err != nil {
		t.Fatalf("ficus %v: %v", args, err)
	}
	return b.String()
}

// The menu waits for min_input characters of a command word and hides once
// the word names a command; Tab (--manual) shows it anyway.
func TestCommandMenuIsHeldBack(t *testing.T) {
	isolate(t, "git", "gitk", "gist", "grep")
	complete := func(extra ...string) string {
		return runOut(t, append([]string{"complete", "--shell", "zsh", "--budget", "0"}, extra...)...)
	}
	if out := complete("--", "g"); !strings.Contains(out, "__ficus_n=0 ") || !strings.Contains(out, "__ficus_held=1") {
		t.Errorf("one letter must be held back:\n%s", out)
	}
	if out := complete("--", "gi"); strings.Contains(out, "__ficus_n=0 ") || !strings.Contains(out, "gist") {
		t.Errorf("two letters list commands:\n%s", out)
	}
	if out := complete("--", "git"); !strings.Contains(out, "__ficus_n=0 ") || !strings.Contains(out, "__ficus_held=1") {
		t.Errorf("a word naming a command hides the menu:\n%s", out)
	}
	if out := complete("--manual", "--", "g"); strings.Contains(out, "__ficus_n=0 ") || !strings.Contains(out, "grep") {
		t.Errorf("--manual shows the menu for one letter:\n%s", out)
	}
}

// Tab with nothing picked inserts what all matches share, without a space;
// when that adds nothing it inserts the first match.
func TestAcceptInsertsCommonPrefix(t *testing.T) {
	dir := isolate(t, "git")
	backend := filepath.Join(dir, "carapace")
	if err := os.WriteFile(backend, []byte(`#!/bin/sh
echo '{"values":[{"value":"checkout"},{"value":"cherry"},{"value":"cherry-pick"}]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FICUS_CARAPACE", backend)
	t.Setenv("FICUS_RECENT", "0") // keep the ranking fixed across calls
	accept := func(line string) string {
		return runOut(t, "accept", "--shell", "zsh", "--unselected", "--budget", "0", "--", line)
	}
	if got := accept("git ch"); got != "__ficus_lbuffer=$'git che'\n" {
		t.Errorf("git ch: %q", got)
	}
	// Ranked cherry, checkout, cherry-pick (shortest first among equals).
	if got := accept("git che"); got != "__ficus_lbuffer=$'git cherry '\n" {
		t.Errorf("git che (nothing more in common): %q", got)
	}
	if got := runOut(t, "accept", "--shell", "zsh", "--budget", "0", "--sel", "1", "--", "git ch"); got != "__ficus_lbuffer=$'git checkout '\n" {
		t.Errorf("a picked item is inserted as it is: %q", got)
	}
}

// Until Up/Down chooses, the first item is highlighted and Enter completes
// with it — unless what is typed is already the line to run, when the "run"
// row on top is highlighted instead and Enter runs the line as typed;
// preselect = false highlights nothing.
func TestPreselect(t *testing.T) {
	dir := isolate(t, "git")
	backend := filepath.Join(dir, "carapace")
	if err := os.WriteFile(backend, []byte(`#!/bin/sh
echo '{"values":[{"value":"checkout"},{"value":"cherry"},{"value":"status"},{"value":"stash"},{"value":"stash-show"}]}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FICUS_CARAPACE", backend)
	complete := func(line string) string {
		return runOut(t, "complete", "--shell", "zsh", "--unselected", "--budget", "0", "--", line)
	}
	if out := complete("git ch"); !strings.Contains(out, "__ficus_presel=1") || !strings.Contains(out, `\n>`) {
		t.Errorf("a typed word highlights its best match:\n%s", out)
	}
	for _, line := range []string{"git ", "git stash"} {
		out := complete(line)
		if !strings.Contains(out, "__ficus_presel=0") || !strings.Contains(out, `\n>↪ run`) {
			t.Errorf("%q: the run row is highlighted, so Enter runs the line:\n%s", line, out)
		}
	}
	if out := runOut(t, "complete", "--shell", "zsh", "--sel", "0", "--budget", "0", "--", "git stash"); !strings.Contains(out, `\n ↪ run`) || !strings.Contains(out, `\n>stash`) {
		t.Errorf("after Down the first item is highlighted below the run row:\n%s", out)
	}
	t.Setenv("FICUS_PRESELECT", "0")
	if out := complete("git ch"); !strings.Contains(out, "__ficus_presel=0") || strings.Contains(out, `\n>`) {
		t.Errorf("preselect = false highlights nothing:\n%s", out)
	}
}
