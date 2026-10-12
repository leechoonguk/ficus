// Package setup manages the ficus block in shell startup files, so every new
// shell (after login or reboot) loads ficus, and removal restores the files.
//
// The block is delimited by marker lines; nothing outside it is touched, and
// a file whose markers do not pair up is left alone with an error. Files are
// rewritten in place, which keeps symlinks and permissions.
package setup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	begin = "# >>> ficus >>>"
	end   = "# <<< ficus <<<"
)

// Env is where setup looks: the home directory and how to run shells. Tests
// replace it to work in a throwaway home.
type Env struct {
	Home   string
	Getenv func(string) string
	// Run runs a program and returns its trimmed stdout; err when it is
	// missing or fails.
	Run func(name string, args ...string) (string, error)
	Out io.Writer // progress messages
}

// System is the real environment.
func System(out io.Writer) Env {
	home, _ := os.UserHomeDir()
	return Env{
		Home:   home,
		Getenv: os.Getenv,
		Run: func(name string, args ...string) (string, error) {
			path, err := exec.LookPath(name)
			if err != nil {
				return "", err
			}
			cmd := exec.Command(path, args...)
			cmd.Stdin = nil
			b, err := cmd.Output()
			return strings.TrimSpace(string(b)), err
		},
		Out: out,
	}
}

// Shells are the names Add accepts.
var Shells = []string{"zsh", "bash", "pwsh", "powershell"}

// Detect lists the shells installed here that ficus supports: zsh 5.9+,
// bash 4+ (macOS ships 3.2), PowerShell 7 (pwsh) and, on Windows, Windows
// PowerShell 5.1 (powershell).
func Detect(e Env) []string {
	var found []string
	if runtime.GOOS != "windows" {
		if _, err := e.Run("zsh", "-fc", "autoload -Uz is-at-least; is-at-least 5.9"); err == nil {
			found = append(found, "zsh")
		}
		if _, err := e.Run("bash", "-c", "(( BASH_VERSINFO[0] >= 4 ))"); err == nil {
			found = append(found, "bash")
		}
	}
	if _, err := e.Run("pwsh", "-NoLogo", "-NoProfile", "-Command", "exit 0"); err == nil {
		found = append(found, "pwsh")
	}
	if runtime.GOOS == "windows" {
		if _, err := e.Run("powershell", "-NoLogo", "-NoProfile", "-Command", "exit 0"); err == nil {
			found = append(found, "powershell")
		}
	}
	return found
}

// target is one startup file and the line that loads ficus from it.
type target struct{ file, line string }

func (e Env) zshRC() string {
	// zsh itself resolves ZDOTDIR (often set in ~/.zshenv, which -c reads).
	if p, err := e.Run("zsh", "-c", "print -r -- ${ZDOTDIR:-$HOME}/.zshrc"); err == nil && p != "" {
		return p
	}
	dir := e.Getenv("ZDOTDIR")
	if dir == "" {
		dir = e.Home
	}
	return filepath.Join(dir, ".zshrc")
}

// bashLoginFile is the file a login bash reads: the first that exists.
func (e Env) bashLoginFile() string {
	for _, f := range []string{".bash_profile", ".bash_login", ".profile"} {
		if p := filepath.Join(e.Home, f); fileExists(p) {
			return p
		}
	}
	return filepath.Join(e.Home, ".bash_profile") // none yet: shadows nothing
}

// bashRCs: ~/.bashrc for non-login shells, plus the login file unless it
// already sources ~/.bashrc (macOS terminals and SSH sessions start login
// shells).
func (e Env) bashRCs() []string {
	rcs := []string{filepath.Join(e.Home, ".bashrc")}
	login := e.bashLoginFile()
	if data, err := os.ReadFile(login); err != nil || !bytes.Contains(data, []byte(".bashrc")) {
		rcs = append(rcs, login)
	}
	return rcs
}

func (e Env) psProfile(exe string) (string, error) {
	return e.Run(exe, "-NoLogo", "-NoProfile", "-Command", "$PROFILE")
}

func sq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" } // POSIX shell
func pq(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }   // PowerShell

// targets lists the startup file and line for each shell. Lines run only
// in their shell and leave the exit status alone.
func (e Env) targets(ficus string, shells []string) ([]target, error) {
	var ts []target
	for _, sh := range shells {
		switch sh {
		case "zsh":
			ts = append(ts, target{e.zshRC(), fmt.Sprintf(`if [ -x %s ]; then eval "$(%s init zsh)"; fi`, sq(ficus), sq(ficus))})
		case "bash":
			// ~/.profile is also read by sh/dash.
			line := fmt.Sprintf(`if [ -n "${BASH_VERSION-}" ] && [ -x %s ]; then eval "$(%s init bash)"; fi`, sq(ficus), sq(ficus))
			for _, f := range e.bashRCs() {
				ts = append(ts, target{f, line})
			}
		case "pwsh", "powershell":
			file, err := e.psProfile(sh)
			if err != nil || file == "" {
				return nil, fmt.Errorf("%s not found", sh)
			}
			ts = append(ts, target{file, fmt.Sprintf("if (Test-Path -LiteralPath %s) { Invoke-Expression (& %s init pwsh | Out-String) }", pq(ficus), pq(ficus))})
		default:
			return nil, fmt.Errorf("unknown shell %q (want %s)", sh, strings.Join(Shells, ", "))
		}
	}
	return ts, nil
}

// Add (re)writes the block that loads ficus (the binary at ficus) for each
// shell. Every target is validated before any is changed, so a malformed
// file never leaves the installation half done.
func Add(e Env, ficus string, shells []string) error {
	ts, err := e.targets(ficus, shells)
	if err != nil {
		return err
	}
	var bad []error
	for _, t := range ts {
		if _, _, err := strip(t.file); errors.Is(err, errUnmatched) {
			bad = append(bad, fmt.Errorf("%s has unmatched %q/%q lines; fix it by hand, nothing changed", t.file, begin, end))
		}
	}
	if len(bad) > 0 {
		return errors.Join(bad...)
	}
	for _, t := range ts {
		if err := remove(t.file, nil); err != nil {
			return err
		}
		if err := appendBlock(t.file, t.line); err != nil {
			return err
		}
		fmt.Fprintf(e.Out, "ficus: enabled in %s\n", t.file)
	}
	return nil
}

// Remove deletes the block from every startup file ficus may have written.
func Remove(e Env) error {
	files := []string{e.zshRC()}
	for _, f := range []string{".bashrc", ".bash_profile", ".bash_login", ".profile"} {
		files = append(files, filepath.Join(e.Home, f))
	}
	for _, sh := range []string{"pwsh", "powershell"} {
		if p, err := e.psProfile(sh); err == nil && p != "" {
			files = append(files, p)
		}
	}
	var errs []error
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		if err := remove(f, e.Out); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

var errUnmatched = errors.New("unmatched markers")

// remove deletes the block from file, restoring it byte for byte (see
// strip). A file that only the block made is deleted.
func remove(file string, out io.Writer) error {
	data, state, err := strip(file)
	switch {
	case errors.Is(err, errUnmatched):
		return fmt.Errorf("%s has unmatched %q/%q lines; fix it by hand, left unchanged", file, begin, end)
	case err != nil:
		return err
	}
	switch state {
	case stripped:
		if err := writeInPlace(file, data); err != nil {
			return err
		}
		if out != nil {
			fmt.Fprintf(out, "ficus: removed from %s\n", file)
		}
	case createdEmpty:
		if err := os.Remove(file); err != nil {
			return err
		}
		if out != nil {
			fmt.Fprintf(out, "ficus: removed %s (created by ficus)\n", file)
		}
	}
	return nil
}

type stripState int

const (
	noBlock stripState = iota
	stripped
	createdEmpty
)

// strip returns file without the block and the blank line written before
// it. Flags on the BEGIN line record what appendBlock had to change besides
// the block: "nl" a final newline it supplied (dropped again only if the
// file still ends with the block), "new" a file it created (deleted again
// if nothing else was added to it). Lines are compared without a trailing
// CR so CRLF files work too.
func strip(file string) ([]byte, stripState, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, noBlock, nil
	}
	if err != nil {
		return nil, noBlock, err
	}
	recs := strings.Split(string(data), "\n")
	if strings.HasSuffix(string(data), "\n") || len(data) == 0 {
		recs = recs[:len(recs)-1] // the final newline ends a line, not a record
	}
	var out []string
	var pending string
	skip, bad, found, blank, done, after, nl, created := false, false, false, false, false, false, false, false
	emit := func(s string) {
		out = append(out, s)
		if done {
			after = true
		}
	}
	for _, rec := range recs {
		line := strings.TrimSuffix(rec, "\r")
		switch {
		case strings.HasPrefix(line, begin) && (line == begin || line[len(begin)] == ' '):
			if skip {
				bad = true
			}
			flags := " " + line[len(begin):] + " "
			nl, created = strings.Contains(flags, " nl "), strings.Contains(flags, " new ")
			skip, found, blank = true, true, false
		case line == end:
			if !skip {
				bad = true
			}
			skip, done = false, true
		case skip:
		case line == "":
			if blank {
				emit(pending)
			}
			pending, blank = rec, true
		default:
			if blank {
				emit(pending)
			}
			blank = false
			emit(rec)
		}
	}
	if skip || bad {
		return nil, noBlock, errUnmatched
	}
	if blank {
		emit(pending)
	}
	if !found {
		return nil, noBlock, nil
	}
	if created && len(out) == 0 {
		return nil, createdEmpty, nil
	}
	result := strings.Join(out, "\n")
	if len(out) > 0 && !(nl && !after) {
		result += "\n"
	}
	return []byte(result), stripped, nil
}

func appendBlock(file, line string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	b := begin
	var prefix string
	switch st, err := os.Stat(file); {
	case errors.Is(err, os.ErrNotExist):
		b += " new"
	case err != nil:
		return err
	case st.Size() > 0:
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if data[len(data)-1] != '\n' {
			// A file without a final newline gets one; remember it for strip.
			prefix = "\n"
			b += " nl"
		}
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(f, "%s\n%s\n%s\n%s\n", prefix, b, line, end)
	return errors.Join(werr, f.Close())
}

// writeInPlace replaces the content of file (through a symlink, keeping its
// mode) rather than the file itself.
func writeInPlace(file string, data []byte) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	return errors.Join(werr, f.Close())
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
