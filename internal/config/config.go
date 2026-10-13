// Package config loads ficus's user settings: defaults, overridden by
// ~/.config/ficus/config.toml, overridden by FICUS_* environment variables.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Settings are the user-facing options. The shell integrations receive the
// ones they need from `ficus init`; the engine loads them on every call.
type Settings struct {
	Color         string // auto | always | never
	Theme         string // ansi | dark | light | auto
	ASCII         string // auto | on | off
	AmbiWidth     int    // 1 | 2: columns of East Asian ambiguous characters
	Icons         string // none | ascii | nerd
	Rows          int    // item rows shown at once
	Details       int    // lines of the selected item's description pane
	Groups        bool   // group items by tag under headers
	MinInput      int    // characters typed in command position before the menu opens
	AutoPopup     bool   // open the menu while typing (false: Tab opens it)
	Preselect     bool   // highlight the first item (or the run row, engine.RunAsTyped): Enter does what is highlighted
	UpArrow       string // history | menu: what Up does before Down picked an item
	Ghost         bool   // inline suggestion after the cursor
	Recent        bool   // rank recently accepted items first
	HelpFlags     bool   // read flags from `cmd --help` for commands carapace does not know
	Budget        time.Duration
	TTL           time.Duration
	PickerPopup   bool
	PickerPreview bool
	DisableFor    []string
	// Keys maps each action in Actions to a key name ("" = unbound).
	Keys map[string]string
}

// Actions are the rebindable actions, in display order.
var Actions = []string{"accept", "pick", "dismiss", "next_group", "prev_group"}

// Defaults returns the built-in settings.
func Defaults() Settings {
	return Settings{
		Color:         "auto",
		Theme:         "ansi",
		ASCII:         "auto",
		AmbiWidth:     1,
		Icons:         "ascii",
		Rows:          8,
		Details:       3,
		Groups:        true,
		MinInput:      2,
		AutoPopup:     true,
		Preselect:     true,
		UpArrow:       "history",
		Ghost:         false,
		Recent:        true,
		HelpFlags:     true,
		Budget:        60 * time.Millisecond,
		TTL:           10 * time.Second,
		PickerPopup:   false,
		PickerPreview: true,
		Keys: map[string]string{
			"accept":     "tab",
			"pick":       "ctrl-space",
			"dismiss":    "ctrl-g",
			"next_group": "alt-down",
			"prev_group": "alt-up",
		},
	}
}

// Path returns the config file location.
func Path() string {
	if p := os.Getenv("FICUS_CONFIG"); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "ficus", "config.toml")
		}
	} else if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ficus", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ficus", "config.toml")
}

// file mirrors the TOML document. Every field is untyped so that one value
// of the wrong type is reported for its key instead of failing the decode.
type file struct {
	Color         any       `toml:"color"`
	Theme         any       `toml:"theme"`
	ASCII         any       `toml:"ascii"`
	AmbiWidth     any       `toml:"ambiwidth"`
	Icons         any       `toml:"icons"`
	Rows          any       `toml:"rows"`
	Details       any       `toml:"details"`
	Groups        any       `toml:"groups"`
	MinInput      any       `toml:"min_input"`
	AutoPopup     any       `toml:"auto_popup"`
	Preselect     any       `toml:"preselect"`
	UpArrow       any       `toml:"up_arrow"`
	Ghost         any       `toml:"ghost"`
	Recent        any       `toml:"recent"`
	HelpFlags     any       `toml:"help_flags"`
	Budget        any       `toml:"budget"`
	TTL           any       `toml:"ttl"`
	PickerPopup   any       `toml:"picker_popup"`
	PickerPreview any       `toml:"picker_preview"`
	DisableFor    any       `toml:"disable_for"`
	Keys          *fileKeys `toml:"keys"`
}

type fileKeys struct {
	Accept    any `toml:"accept"`
	Pick      any `toml:"pick"`
	Dismiss   any `toml:"dismiss"`
	NextGroup any `toml:"next_group"`
	PrevGroup any `toml:"prev_group"`
}

// Load returns the settings and the config file path. The settings are
// always usable: a key that fails validation keeps its default, and the
// error ("config <path>: ...") lists every problem. A missing file is not
// an error.
func Load() (Settings, string, error) {
	path := Path()
	s := Defaults()
	var problems []string
	report := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	var f file
	if data, err := os.ReadFile(path); err == nil {
		dec := toml.NewDecoder(strings.NewReader(string(data))).DisallowUnknownFields()
		if err := dec.Decode(&f); err != nil {
			var strict *toml.StrictMissingError
			var derr *toml.DecodeError
			switch {
			case errors.As(err, &strict):
				for _, e := range strict.Errors {
					report("unknown key %s", strings.Join(e.Key(), "."))
				}
			case errors.As(err, &derr):
				row, col := derr.Position()
				report("line %d column %d: %s", row, col, derr.Error())
				f = file{}
			default:
				report("%v", err)
				f = file{}
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		report("%v", err)
	}

	str := func(key string, fv any, env string, dst *string, allowed ...string) {
		set := func(origin, v string) {
			for _, a := range allowed {
				if v == a {
					*dst = v
					return
				}
			}
			report("%s: %q is not one of %s", origin, v, strings.Join(allowed, ", "))
		}
		if fv != nil {
			if v, ok := fv.(string); ok {
				set(key, v)
			} else {
				report("%s: want a string", key)
			}
		}
		if v, ok := os.LookupEnv(env); ok && v != "" {
			set(env, v)
		}
	}
	num := func(key string, fv any, env string, dst *int, lo, hi int) {
		set := func(origin string, v int64) {
			if v < int64(lo) || v > int64(hi) {
				report("%s: %d is not within %d-%d", origin, v, lo, hi)
				return
			}
			*dst = int(v)
		}
		if fv != nil {
			if v, ok := fv.(int64); ok {
				set(key, v)
			} else {
				report("%s: want an integer", key)
			}
		}
		if v, ok := os.LookupEnv(env); ok && v != "" {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				set(env, n)
			} else {
				report("%s: %q is not an integer", env, v)
			}
		}
	}
	flag := func(key string, fv any, env string, dst *bool) {
		if fv != nil {
			if v, ok := fv.(bool); ok {
				*dst = v
			} else {
				report("%s: want true or false", key)
			}
		}
		if v, ok := os.LookupEnv(env); ok && v != "" {
			if b, ok := ParseBool(v); ok {
				*dst = b
			} else {
				report("%s: %q is not one of 1, 0, true, false, on, off", env, v)
			}
		}
	}
	dur := func(key string, fv any, env string, dst *time.Duration) {
		set := func(origin, v string) {
			d, err := time.ParseDuration(v)
			if err != nil || d < 0 {
				report("%s: %q is not a duration such as 60ms or 10s", origin, v)
				return
			}
			*dst = d
		}
		if fv != nil {
			if v, ok := fv.(string); ok {
				set(key, v)
			} else {
				report("%s: want a duration string such as \"60ms\"", key)
			}
		}
		if v, ok := os.LookupEnv(env); ok && v != "" {
			set(env, v)
		}
	}

	str("color", f.Color, "FICUS_COLOR", &s.Color, "auto", "always", "never")
	str("theme", f.Theme, "FICUS_THEME", &s.Theme, "ansi", "dark", "light", "auto")
	str("ascii", f.ASCII, "FICUS_ASCII", &s.ASCII, "auto", "on", "off")
	num("ambiwidth", f.AmbiWidth, "FICUS_AMBIWIDTH", &s.AmbiWidth, 1, 2)
	str("icons", f.Icons, "FICUS_ICONS", &s.Icons, "none", "ascii", "nerd")
	num("rows", f.Rows, "FICUS_ROWS", &s.Rows, 1, 50)
	num("details", f.Details, "FICUS_DETAILS", &s.Details, 0, 10)
	flag("groups", f.Groups, "FICUS_GROUPS", &s.Groups)
	num("min_input", f.MinInput, "FICUS_MIN_INPUT", &s.MinInput, 0, 10)
	flag("auto_popup", f.AutoPopup, "FICUS_AUTO_POPUP", &s.AutoPopup)
	flag("preselect", f.Preselect, "FICUS_PRESELECT", &s.Preselect)
	str("up_arrow", f.UpArrow, "FICUS_UP_ARROW", &s.UpArrow, "history", "menu")
	flag("ghost", f.Ghost, "FICUS_GHOST", &s.Ghost)
	flag("recent", f.Recent, "FICUS_RECENT", &s.Recent)
	flag("help_flags", f.HelpFlags, "FICUS_HELP_FLAGS", &s.HelpFlags)
	dur("budget", f.Budget, "FICUS_BUDGET", &s.Budget)
	dur("ttl", f.TTL, "FICUS_TTL", &s.TTL)
	flag("picker_popup", f.PickerPopup, "FICUS_PICKER_POPUP", &s.PickerPopup)
	flag("picker_preview", f.PickerPreview, "FICUS_PICKER_PREVIEW", &s.PickerPreview)

	if f.DisableFor != nil {
		if list, ok := f.DisableFor.([]any); ok {
			var names []string
			for _, v := range list {
				if name, ok := v.(string); ok {
					names = append(names, name)
				} else {
					report("disable_for: want a list of strings")
					names = nil
					break
				}
			}
			s.DisableFor = names
		} else {
			report("disable_for: want a list of strings")
		}
	}
	if v, ok := os.LookupEnv("FICUS_DISABLE_FOR"); ok && v != "" {
		s.DisableFor = nil
		for name := range strings.SplitSeq(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				s.DisableFor = append(s.DisableFor, name)
			}
		}
	}

	var fk fileKeys
	if f.Keys != nil {
		fk = *f.Keys
	}
	fileKey := map[string]any{
		"accept": fk.Accept, "pick": fk.Pick, "dismiss": fk.Dismiss,
		"next_group": fk.NextGroup, "prev_group": fk.PrevGroup,
	}
	keys := map[string]string{}
	for _, action := range Actions {
		keys[action] = s.Keys[action]
		setKey := func(origin, v string) {
			if v != "" {
				if err := ValidKey(v); err != nil {
					report("%s: %v", origin, err)
					return
				}
			}
			keys[action] = v
		}
		if fv := fileKey[action]; fv != nil {
			if v, ok := fv.(string); ok {
				setKey("keys."+action, v)
			} else {
				report("keys.%s: want a key name string", action)
			}
		}
		// Set but empty unbinds the action.
		if v, ok := os.LookupEnv("FICUS_KEY_" + strings.ToUpper(action)); ok {
			setKey("FICUS_KEY_"+strings.ToUpper(action), v)
		}
	}
	if err := checkDuplicates(keys); err != nil {
		report("%v", err)
	} else {
		s.Keys = keys
	}

	if len(problems) > 0 {
		return s, path, fmt.Errorf("config %s: %s", path, strings.Join(problems, "; "))
	}
	return s, path, nil
}

// checkDuplicates rejects two actions bound to the same key.
func checkDuplicates(keys map[string]string) error {
	owner := map[string]string{}
	for _, action := range Actions {
		k := keys[action]
		if k == "" {
			continue
		}
		if prev, ok := owner[k]; ok {
			return fmt.Errorf("keys: %s and %s share %s", prev, action, k)
		}
		owner[k] = action
	}
	return nil
}

// ParseBool accepts 1/0, true/false and on/off, in any case.
func ParseBool(v string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on":
		return true, true
	case "0", "false", "off":
		return false, true
	}
	return false, false
}
