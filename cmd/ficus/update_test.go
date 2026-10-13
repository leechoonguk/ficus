package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeRelease serves what GitHub does for a release: /releases/latest
// redirects to the tag, and the archive and checksums.txt are downloadable.
type fakeRelease struct {
	tag, archive string
	data         []byte
	badSum       bool
	downloads    atomic.Int32
}

func (f *fakeRelease) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dl := "/releases/download/" + f.tag + "/"
	switch r.URL.Path {
	case "/releases/latest":
		http.Redirect(w, r, "/releases/tag/"+f.tag, http.StatusFound)
	case dl + f.archive:
		f.downloads.Add(1)
		w.Write(f.data)
	case dl + "checksums.txt":
		sum := sha256.Sum256(f.data)
		if f.badSum {
			sum[0] ^= 1
		}
		io.WriteString(w, "0000  other_file.tar.gz\n"+hex.EncodeToString(sum[:])+"  "+f.archive+"\n")
	default:
		http.NotFound(w, r)
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		io.WriteString(tw, body)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipped(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	zw.Close()
	return buf.Bytes()
}

// installed puts old binaries in a fresh directory, as install.sh leaves them.
func installed(t *testing.T, names ...string) string {
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old "+n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func contents(t *testing.T, dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newUpdater(srv *httptest.Server, goos string, out io.Writer) updater {
	return updater{base: srv.URL, client: srv.Client(), goos: goos, goarch: "arm64", out: out}
}

func TestUpdate(t *testing.T) {
	newBins := map[string]string{"ficus": "new ficus", "ficus-carapace": "new carapace", "README.md": "readme"}

	t.Run("newer release replaces both binaries", func(t *testing.T) {
		rel := &fakeRelease{tag: "v0.2.0", archive: "ficus_linux_arm64.tar.gz", data: tarGz(t, newBins)}
		srv := httptest.NewServer(rel)
		defer srv.Close()
		dir := installed(t, "ficus", "ficus-carapace")
		var out strings.Builder
		if err := newUpdater(srv, "linux", &out).run(dir, "0.1.0", false, false); err != nil {
			t.Fatal(err)
		}
		if got := contents(t, dir, "ficus"); got != "new ficus" {
			t.Errorf("ficus = %q", got)
		}
		if got := contents(t, dir, "ficus-carapace"); got != "new carapace" {
			t.Errorf("ficus-carapace = %q", got)
		}
		// Windows file systems have no execute bit to check.
		if fi, _ := os.Stat(filepath.Join(dir, "ficus")); runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
			t.Errorf("ficus is not executable: %v", fi.Mode())
		}
		if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
			t.Error("files other than the binaries were installed")
		}
		if !strings.Contains(out.String(), "updated to v0.2.0") {
			t.Errorf("output: %q", out.String())
		}
	})

	t.Run("checksum mismatch leaves the install alone", func(t *testing.T) {
		rel := &fakeRelease{tag: "v0.2.0", archive: "ficus_linux_arm64.tar.gz", data: tarGz(t, newBins), badSum: true}
		srv := httptest.NewServer(rel)
		defer srv.Close()
		dir := installed(t, "ficus", "ficus-carapace")
		err := newUpdater(srv, "linux", io.Discard).run(dir, "0.1.0", false, false)
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("err = %v", err)
		}
		if got := contents(t, dir, "ficus"); got != "old ficus" {
			t.Errorf("ficus = %q", got)
		}
	})

	t.Run("up to date, check only, or a source build downloads nothing", func(t *testing.T) {
		rel := &fakeRelease{tag: "v0.2.0", archive: "ficus_linux_arm64.tar.gz", data: tarGz(t, newBins)}
		srv := httptest.NewServer(rel)
		defer srv.Close()
		dir := installed(t, "ficus", "ficus-carapace")
		u := newUpdater(srv, "linux", io.Discard)
		for _, c := range []struct {
			current string
			check   bool
			wantErr bool
		}{
			{"0.2.0", false, false},                  // already the latest
			{"v0.3.0", false, false},                 // newer than the latest
			{"0.1.0", true, false},                   // --check
			{"v0.1.0-7-gbc3007d-dirty", false, true}, // make build: needs --force
			{"develop", true, false},
		} {
			err := u.run(dir, c.current, c.check, false)
			if (err != nil) != c.wantErr {
				t.Errorf("%s check=%v: err = %v", c.current, c.check, err)
			}
		}
		if n := rel.downloads.Load(); n != 0 {
			t.Errorf("archive downloaded %d times", n)
		}
		if got := contents(t, dir, "ficus"); got != "old ficus" {
			t.Errorf("ficus = %q", got)
		}
		// --force replaces a source build with the release.
		if err := u.run(dir, "v0.1.0-7-gbc3007d-dirty", false, true); err != nil {
			t.Fatal(err)
		}
		if got := contents(t, dir, "ficus"); got != "new ficus" {
			t.Errorf("after --force ficus = %q", got)
		}
	})

	t.Run("windows moves the running exe aside", func(t *testing.T) {
		rel := &fakeRelease{tag: "v0.2.0", archive: "ficus_windows_arm64.zip",
			data: zipped(t, map[string]string{"ficus.exe": "new ficus", "ficus-carapace.exe": "new carapace"})}
		srv := httptest.NewServer(rel)
		defer srv.Close()
		dir := installed(t, "ficus.exe", "ficus-carapace.exe", "ficus.exe.old")
		if err := newUpdater(srv, "windows", io.Discard).run(dir, "0.1.0", false, false); err != nil {
			t.Fatal(err)
		}
		if got := contents(t, dir, "ficus.exe"); got != "new ficus" {
			t.Errorf("ficus.exe = %q", got)
		}
		// The previous update's leftover is gone; this update's old exe waits.
		if got := contents(t, dir, "ficus.exe.old"); got != "old ficus.exe" {
			t.Errorf("ficus.exe.old = %q", got)
		}
	})
}

func TestVersionOrder(t *testing.T) {
	older, _ := parseVersion("0.9.0")
	newer, ok := parseVersion("v0.10.0")
	if !ok || compareVersions(newer, older) <= 0 {
		t.Errorf("v0.10.0 must be newer than 0.9.0")
	}
	for _, s := range []string{"v0.1.0-7-gbc3007d-dirty", "develop", "v1.2", "1.2.3.4", "v01.2.3"} {
		if _, ok := parseVersion(s); ok {
			t.Errorf("%q parsed as a release version", s)
		}
	}
}
