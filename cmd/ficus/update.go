package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// releaseRepo is where install.sh and install.ps1 download from.
const releaseRepo = "https://github.com/leechoonguk/ficus"

// updater replaces the installed binaries with a GitHub release, the way
// install.sh and install.ps1 install them: the same archive, verified against
// the release's checksums.txt. Tests point base at a local server.
type updater struct {
	base         string
	client       *http.Client
	goos, goarch string
	out          io.Writer
}

// runUpdate is `ficus update`: install the latest release over this ficus
// and the ficus-carapace next to it.
func runUpdate(args []string, out io.Writer) error {
	fl := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fl.Bool("check", false, "only report whether a newer release exists")
	force := fl.Bool("force", false, "install the latest release even over a source build or the same version")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() > 0 {
		return fmt.Errorf("update: unexpected argument %q", fl.Arg(0))
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// A symlink (Homebrew-style link) points at the real install directory.
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	u := updater{
		base:   releaseRepo,
		client: &http.Client{Timeout: 5 * time.Minute},
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
		out:    out,
	}
	return u.run(filepath.Dir(exe), version, *check, *force)
}

func (u updater) run(dir, current string, check, force bool) error {
	u.removeOld(dir)
	tag, err := u.latest()
	if err != nil {
		return err
	}
	newest, ok := parseVersion(tag)
	if !ok {
		return fmt.Errorf("update: the latest release has an unexpected tag %q", tag)
	}
	cur, release := parseVersion(current)
	switch {
	case !release:
		fmt.Fprintf(u.out, "ficus: this is a source build (%s); the latest release is %s\n", current, tag)
		if check {
			return nil
		}
		if !force {
			return errors.New("update: a source build updates with git pull && make build && make install; to replace it with the release: ficus update --force")
		}
	case compareVersions(newest, cur) <= 0:
		fmt.Fprintf(u.out, "ficus: %s is the latest release\n", tag)
		if check || !force {
			return nil
		}
	case check:
		fmt.Fprintf(u.out, "ficus: %s is available (this is %s); run: ficus update\n", tag, current)
		return nil
	}
	if u.goos == "linux" && dir == "/usr/bin" {
		return errors.New("update: /usr/bin/ficus belongs to the .deb or .rpm package; update it by installing the new package")
	}
	if err := writable(dir); err != nil {
		return fmt.Errorf("update: cannot write to %s: %w", dir, err)
	}
	bins, err := u.download(tag)
	if err != nil {
		return err
	}
	// ficus-carapace first: a ficus left at the old version still works with
	// either, and ficus itself is what reports the version.
	for _, name := range []string{u.exe("ficus-carapace"), u.exe("ficus")} {
		if err := u.replace(dir, name, bins[name]); err != nil {
			return err
		}
	}
	fmt.Fprintf(u.out, "ficus: updated to %s in %s; open a new terminal to load it\n", tag, dir)
	return nil
}

// latest reads the newest release tag from the redirect of /releases/latest
// (…/releases/tag/vX.Y.Z), which needs no API token and has no rate limit.
func (u updater) latest() (string, error) {
	c := *u.client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Get(u.base + "/releases/latest")
	if err != nil {
		return "", fmt.Errorf("update: looking up the latest release: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || !strings.Contains(loc, "/releases/tag/") {
		return "", fmt.Errorf("update: looking up the latest release: %s gave %s, not a redirect to a release", u.base+"/releases/latest", resp.Status)
	}
	return path.Base(loc), nil
}

// download fetches this platform's archive of release tag, checks it against
// checksums.txt and returns the two binaries in it by file name.
func (u updater) download(tag string) (map[string][]byte, error) {
	archive := "ficus_" + u.goos + "_" + u.goarch + ".tar.gz"
	if u.goos == "windows" {
		archive = "ficus_" + u.goos + "_" + u.goarch + ".zip"
	}
	base := u.base + "/releases/download/" + tag + "/"
	fmt.Fprintf(u.out, "ficus: downloading %s%s\n", base, archive)
	data, err := u.get(base + archive)
	if err != nil {
		return nil, err
	}
	sums, err := u.get(base + "checksums.txt")
	if err != nil {
		return nil, err
	}
	want := ""
	for line := range strings.Lines(string(sums)) {
		if f := strings.Fields(line); len(f) == 2 && f[1] == archive {
			want = f[0]
			break
		}
	}
	if want == "" {
		return nil, fmt.Errorf("update: %s is not listed in checksums.txt", archive)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != want {
		return nil, fmt.Errorf("update: checksum mismatch for %s", archive)
	}
	bins, err := unpack(data, u.goos == "windows")
	if err != nil {
		return nil, fmt.Errorf("update: %s: %w", archive, err)
	}
	for _, name := range []string{u.exe("ficus"), u.exe("ficus-carapace")} {
		if bins[name] == nil {
			return nil, fmt.Errorf("update: %s has no %s", archive, name)
		}
	}
	return bins, nil
}

func (u updater) get(url string) ([]byte, error) {
	resp, err := u.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("update: %s: %w", url, err)
	}
	return data, nil
}

// unpack returns the regular files at the top of a release archive (a zip on
// Windows, a gzipped tar elsewhere) whose names are ficus binaries.
func unpack(data []byte, isZip bool) (map[string][]byte, error) {
	bins := map[string][]byte{}
	keep := func(name string) bool {
		switch name {
		case "ficus", "ficus-carapace", "ficus.exe", "ficus-carapace.exe":
			return true
		}
		return false
	}
	if isZip {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if !keep(f.Name) || !f.Mode().IsRegular() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			bins[f.Name] = b
		}
		return bins, nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return bins, nil
		}
		if err != nil {
			return nil, err
		}
		if !keep(h.Name) || h.Typeflag != tar.TypeReg {
			continue
		}
		if bins[h.Name], err = io.ReadAll(tr); err != nil {
			return nil, err
		}
	}
}

// replace swaps dir/name for data without ever leaving it half written: the
// new file is written beside it and renamed over it, so a running ficus keeps
// its old file. Windows cannot replace a running .exe but can rename it, so
// the old one moves aside to name.old (removed by the next update).
func (u updater) replace(dir, name string, data []byte) error {
	dst := filepath.Join(dir, name)
	tmp := filepath.Join(dir, "."+name+".new")
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if u.goos == "windows" {
		old := dst + ".old"
		os.Remove(old)
		if err := os.Rename(dst, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
			os.Remove(tmp)
			return fmt.Errorf("update: %w", err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("update: %w", err)
	}
	return nil
}

// removeOld deletes the .old executables a previous Windows update moved
// aside; one still running stays until the next time.
func (u updater) removeOld(dir string) {
	if u.goos != "windows" {
		return
	}
	for _, name := range []string{"ficus.exe.old", "ficus-carapace.exe.old"} {
		os.Remove(filepath.Join(dir, name))
	}
}

func (u updater) exe(name string) string {
	if u.goos == "windows" {
		return name + ".exe"
	}
	return name
}

func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".ficus-update-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

// parseVersion reads a release version, "v1.2.3" or "1.2.3" (GoReleaser
// stamps it without the v). Anything else, such as git describe's
// "v0.1.0-7-gbc3007d-dirty" from make build, is not a release.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compareVersions(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
