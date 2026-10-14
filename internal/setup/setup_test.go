package setup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testEnv is a throwaway home where only the listed shells "exist"; the
// real startup files are never touched. zsh's startup file resolves to
// $HOME/.zshrc and PowerShell's profile to a fixed file in the home.
func testEnv(t *testing.T, shells ...string) Env {
	t.Helper()
	home := t.TempDir()
	return Env{
		Home:   home,
		Getenv: func(string) string { return "" },
		Run: func(name string, args ...string) (string, error) {
			for _, s := range shells {
				if s != name {
					continue
				}
				switch {
				case strings.Contains(strings.Join(args, " "), "$PROFILE"):
					return filepath.Join(home, "Documents", name+"_profile.ps1"), nil
				case name == "zsh" && strings.Contains(strings.Join(args, " "), "print"):
					return filepath.Join(home, ".zshrc"), nil
				}
				return "", nil
			}
			return "", errors.New("not installed")
		},
		Out: io.Discard,
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Add then Remove restores every file byte for byte, CRLF and a missing
// final newline included, and keeps symlinks.
func TestAddRemoveRestoresFiles(t *testing.T) {
	e := testEnv(t)
	dot := filepath.Join(e.Home, "dot")
	if err := os.Mkdir(dot, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "export A=1\r\nalias b=2"
	write(t, filepath.Join(dot, "bashrc"), orig)
	if err := os.Symlink(filepath.Join(dot, "bashrc"), filepath.Join(e.Home, ".bashrc")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Home, ".profile"), "x=1\n")
	for range 2 {
		if err := Add(e, "/opt/ficus/bin/ficus", []string{"bash"}); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(read(t, filepath.Join(e.Home, ".bashrc")), ">>> ficus"); n != 1 {
		t.Errorf("re-add left %d blocks", n)
	}
	if !strings.Contains(read(t, filepath.Join(e.Home, ".profile")), ">>> ficus") {
		t.Error("login file not set up")
	}
	if err := Remove(e); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dot, "bashrc")); got != orig {
		t.Errorf(".bashrc not restored: %q", got)
	}
	if got := read(t, filepath.Join(e.Home, ".profile")); got != "x=1\n" {
		t.Errorf(".profile not restored: %q", got)
	}
	if st, err := os.Lstat(filepath.Join(e.Home, ".bashrc")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced")
	}
}

// A start marker without an end marker: refuse, change nothing. A malformed
// target stops Add before any file is changed.
func TestUnmatchedMarkersChangeNothing(t *testing.T) {
	e := testEnv(t)
	broken := "a=1\n# >>> ficus >>>\nold\nb=2\nKEEP=1\n"
	rc := filepath.Join(e.Home, ".bashrc")
	write(t, rc, broken)
	if Remove(e) == nil {
		t.Error("remove succeeded on unmatched markers")
	}
	if Add(e, "/opt/ficus/bin/ficus", []string{"bash"}) == nil {
		t.Error("add succeeded on unmatched markers")
	}
	if read(t, rc) != broken {
		t.Error("unmatched markers: file changed")
	}

	e = testEnv(t)
	write(t, filepath.Join(e.Home, ".bashrc"), "a=1\n")
	write(t, filepath.Join(e.Home, ".bash_profile"), "# >>> ficus >>>\nbroken\n")
	if Add(e, "/opt/ficus/bin/ficus", []string{"bash"}) == nil {
		t.Error("add succeeded with a malformed login file")
	}
	if read(t, filepath.Join(e.Home, ".bashrc")) != "a=1\n" {
		t.Error("add changed .bashrc before failing on .bash_profile")
	}
}

// Lines another installer appends after the block survive with their final
// newline, even when the file originally had none.
func TestLinesAfterBlockSurvive(t *testing.T) {
	e := testEnv(t)
	rc := filepath.Join(e.Home, ".bashrc")
	write(t, rc, "alias a=1")
	write(t, filepath.Join(e.Home, ".bash_profile"), ". ~/.bashrc\n")
	if err := Add(e, "/opt/ficus/bin/ficus", []string{"bash"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("export Y=1\n")
	f.Close()
	if err := Remove(e); err != nil {
		t.Fatal(err)
	}
	if got := read(t, rc); got != "alias a=1\nexport Y=1\n" {
		t.Errorf("got %q", got)
	}
}

// Files that Add created are deleted again by Remove, the PowerShell
// profile's directory included in what Add creates.
func TestCreatedFilesAreRemoved(t *testing.T) {
	e := testEnv(t, "pwsh")
	if err := Add(e, "/opt/ficus/bin/ficus", []string{"bash", "pwsh"}); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(e.Home, "Documents", "pwsh_profile.ps1")
	for _, f := range []string{".bashrc", ".bash_profile"} {
		if _, err := os.Stat(filepath.Join(e.Home, f)); err != nil {
			t.Errorf("add did not create %s", f)
		}
	}
	if !strings.Contains(read(t, profile), "Invoke-Expression (& '/opt/ficus/bin/ficus' init pwsh") {
		t.Errorf("profile: %q", read(t, profile))
	}
	if err := Remove(e); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(e.Home, ".bashrc"), filepath.Join(e.Home, ".bash_profile"), profile} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("remove left %s", f)
		}
	}
}

// The lines are valid shell code with quotes in the path, inert in shells
// they do not target (~/.profile is read by sh), and leave the exit status
// alone when ficus is missing.
func TestLinesRunInRealShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shells")
	}
	e := testEnv(t)
	write(t, filepath.Join(e.Home, ".profile"), "x=1\n")
	if err := Add(e, "/Users/O'Brien/bin/ficus", []string{"bash", "zsh"}); err != nil {
		t.Fatal(err)
	}
	if bash, err := exec.LookPath("bash"); err == nil {
		if out, err := exec.Command(bash, "-n", filepath.Join(e.Home, ".bashrc")).CombinedOutput(); err != nil {
			t.Errorf("quoted path broke .bashrc: %v %s", err, out)
		}
	}
	cmd := exec.Command("sh", "-c", `. "$HOME/.profile" && echo ok`)
	cmd.Env = append(os.Environ(), "HOME="+e.Home)
	if out, _ := cmd.CombinedOutput(); strings.TrimSpace(string(out)) != "ok" {
		t.Errorf(".profile breaks under sh: %q", out)
	}
	if zsh, err := exec.LookPath("zsh"); err == nil {
		cmd := exec.Command(zsh, "-c", `. "$HOME/.zshrc"; echo $?`)
		cmd.Env = append(os.Environ(), "HOME="+e.Home, "ZDOTDIR="+e.Home)
		if out, _ := cmd.CombinedOutput(); !bytes.Equal(bytes.TrimSpace(out), []byte("0")) {
			t.Errorf("zsh line with ficus missing sets status %q", out)
		}
	}
}

// With ficus installed, the line puts its directory on PATH once (a fresh
// macOS PATH lacks ~/.local/bin) and leaves a PATH that has it alone, even
// when the startup file is read again.
func TestLinesAddDirectoryToPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shells")
	}
	e := testEnv(t)
	dir := filepath.Join(e.Home, "my bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ficus := filepath.Join(dir, "ficus")
	write(t, ficus, "#!/bin/sh\n") // `ficus init` prints nothing
	if err := os.Chmod(ficus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Add(e, ficus, []string{"bash", "zsh"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ shell, rc string }{{"zsh", ".zshrc"}, {"bash", ".bashrc"}} {
		sh, err := exec.LookPath(c.shell)
		if err != nil {
			continue
		}
		rc := filepath.Join(e.Home, c.rc)
		for _, start := range []string{"/usr/bin:/bin", "/usr/bin:" + dir + ":/bin"} {
			cmd := exec.Command(sh, "-c", `. "$1"; . "$1"; printf %s "$PATH"`, "sh", rc)
			cmd.Env = []string{"HOME=" + e.Home, "ZDOTDIR=" + e.Home, "PATH=" + start}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v %s", c.shell, err, out)
			}
			want := start
			if !strings.Contains(start, dir) {
				want = start + ":" + dir
			}
			if string(out) != want {
				t.Errorf("%s with PATH=%s: got %q, want %q", c.shell, start, out, want)
			}
		}
	}
}
