package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FICUS_CONFIG", path)
	return path
}

func TestPrecedenceEnvOverFileOverDefault(t *testing.T) {
	writeConfig(t, "rows = 5\ntheme = \"dark\"\nbudget = \"20ms\"\n[keys]\npick = \"alt-p\"\n")
	t.Setenv("FICUS_ROWS", "12")
	t.Setenv("FICUS_KEY_DISMISS", "")
	s, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Rows != 12 {
		t.Errorf("env must override the file: rows %d", s.Rows)
	}
	if s.Theme != "dark" || s.Budget != 20*time.Millisecond || s.Keys["pick"] != "alt-p" {
		t.Errorf("file must override defaults: theme %q budget %v pick %q", s.Theme, s.Budget, s.Keys["pick"])
	}
	if s.MinInput != 2 || s.UpArrow != "history" {
		t.Errorf("unset keys keep defaults: %+v", s)
	}
	if s.Keys["dismiss"] != "" {
		t.Errorf("an empty FICUS_KEY_ variable unbinds the action, got %q", s.Keys["dismiss"])
	}
}

func TestBadValuesKeepDefaultsAndReportEveryKey(t *testing.T) {
	path := writeConfig(t, "rows = 3\ncolour = \"never\"\ndetails = 99\n")
	t.Setenv("FICUS_GHOST", "maybe")
	s, got, err := Load()
	if got != path || err == nil {
		t.Fatalf("path %q err %v", got, err)
	}
	for _, want := range []string{"config " + path + ":", "colour", "details", "FICUS_GHOST"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if s.Rows != 3 {
		t.Errorf("valid keys still load next to an unknown one: rows %d", s.Rows)
	}
	if s.Details != 3 || s.Ghost {
		t.Errorf("invalid values keep defaults: details %d ghost %v", s.Details, s.Ghost)
	}
}

func TestKeyNamesAreValidated(t *testing.T) {
	for _, name := range []string{"ctrl-x", "ctrl-m", "hyper-k", "alt-"} {
		if ValidKey(name) == nil {
			t.Errorf("%q must be rejected", name)
		}
	}
	writeConfig(t, "[keys]\ndismiss = \"ctrl-x\"\n")
	if s, _, err := Load(); err == nil || s.Keys["dismiss"] != "ctrl-g" {
		t.Errorf("ctrl-x: err %v dismiss %q", err, s.Keys["dismiss"])
	}
	writeConfig(t, "[keys]\npick = \"tab\"\n")
	s, _, err := Load()
	if err == nil || !strings.Contains(err.Error(), "keys: accept and pick share tab") {
		t.Errorf("duplicate binding: %v", err)
	}
	if s.Keys["accept"] != "tab" || s.Keys["pick"] != "ctrl-space" {
		t.Errorf("a conflicting key table falls back to the defaults: %v", s.Keys)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("FICUS_CONFIG", filepath.Join(t.TempDir(), "none.toml"))
	if _, _, err := Load(); err != nil {
		t.Error(err)
	}
}
