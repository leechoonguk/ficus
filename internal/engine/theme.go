package engine

import (
	"runtime"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Style is a terminal text style. Colors 0-15 are the terminal's palette
// (which follows its light/dark scheme), 16-255 fixed 256-color indexes; -1
// leaves the terminal default.
type Style struct {
	Fg, Bg                   int
	Bold, Underline, Reverse bool
}

var noStyle = Style{Fg: -1, Bg: -1}

// Theme colors the menu.
type Theme struct {
	Label, Match, Desc, Status, Error Style
	Selected                          Style // row background and label
	SelectedDesc                      Style
	Header                            Style // group headers
	Kind                              Style // item kind icons
	Ghost                             Style // inline suggestion
}

// ThemeDark uses fixed 256-color indexes tuned for dark backgrounds.
var ThemeDark = Theme{
	Label:        noStyle,
	Match:        Style{Fg: 214, Bg: -1, Bold: true},
	Desc:         Style{Fg: 245, Bg: -1},
	Status:       Style{Fg: 242, Bg: -1},
	Error:        Style{Fg: 167, Bg: -1},
	Selected:     Style{Fg: 255, Bg: 25, Bold: true},
	SelectedDesc: Style{Fg: 252, Bg: 25},
	Header:       Style{Fg: 242, Bg: -1},
	Kind:         Style{Fg: 242, Bg: -1},
	Ghost:        Style{Fg: 242, Bg: -1},
}

// ThemeLight uses fixed 256-color indexes tuned for light backgrounds.
var ThemeLight = Theme{
	Label:        noStyle,
	Match:        Style{Fg: 166, Bg: -1, Bold: true},
	Desc:         Style{Fg: 240, Bg: -1},
	Status:       Style{Fg: 244, Bg: -1},
	Error:        Style{Fg: 160, Bg: -1},
	Selected:     Style{Fg: 16, Bg: 153, Bold: true},
	SelectedDesc: Style{Fg: 236, Bg: 153},
	Header:       Style{Fg: 244, Bg: -1},
	Kind:         Style{Fg: 97, Bg: -1},
	Ghost:        Style{Fg: 248, Bg: -1},
}

// ThemeANSI uses only the 8 base palette colors (plus bright black for the
// ghost), so it follows the terminal's own scheme, light or dark.
var ThemeANSI = Theme{
	Label:        noStyle,
	Match:        Style{Fg: 2, Bg: -1, Bold: true},
	Desc:         noStyle,
	Status:       noStyle,
	Error:        Style{Fg: 1, Bg: -1},
	Selected:     Style{Fg: -1, Bg: -1, Reverse: true, Bold: true},
	SelectedDesc: Style{Fg: -1, Bg: -1, Reverse: true},
	Header:       Style{Fg: -1, Bg: -1, Bold: true},
	Kind:         noStyle,
	Ghost:        Style{Fg: 8, Bg: -1},
}

// ThemeMono uses attributes only (NO_COLOR, terminals without colors).
var ThemeMono = Theme{
	Label:        noStyle,
	Match:        Style{Fg: -1, Bg: -1, Underline: true, Bold: true},
	Desc:         noStyle,
	Status:       noStyle,
	Error:        Style{Fg: -1, Bg: -1, Bold: true},
	Selected:     Style{Fg: -1, Bg: -1, Reverse: true, Bold: true},
	SelectedDesc: Style{Fg: -1, Bg: -1, Reverse: true},
	Header:       Style{Fg: -1, Bg: -1, Bold: true},
	Kind:         noStyle,
	Ghost:        noStyle,
}

// ResolveTheme picks the theme for theme name (dark, light, ansi), the
// color setting (auto, always, never) and the terminal's color count.
// ghostOK reports whether the theme can draw a dim ghost suggestion.
func ResolveTheme(name, colorSetting string, colors int, getenv func(string) string) (th Theme, ghostOK bool) {
	mode := colorSetting
	if mode != "always" && mode != "never" {
		switch {
		case getenv("NO_COLOR") != "":
			mode = "never"
		case getenv("FORCE_COLOR") != "":
			mode = "always"
		default:
			mode = "auto"
		}
	}
	switch {
	case mode == "never":
		return ThemeMono, false
	case mode == "always":
		colors = max(colors, 8)
	case colors < 8:
		return ThemeMono, false
	}
	if colors >= 256 {
		switch name {
		case "dark":
			return ThemeDark, true
		case "light":
			return ThemeLight, true
		}
	}
	return ThemeANSI, colors >= 16
}

// Glyphs are the decorative characters, in Unicode or ASCII.
type Glyphs struct {
	Ellipsis, Rule, Sep string
}

var (
	GlyphsUnicode = Glyphs{Ellipsis: "…", Rule: "─", Sep: "·"}
	GlyphsASCII   = Glyphs{Ellipsis: "...", Rule: "-", Sep: "|"}
)

// ResolveGlyphs chooses ASCII when asked to (on), or (auto) when the locale
// is not UTF-8, on the Linux console, or when ambiguous-width characters
// are two columns wide (… would be misplaced).
func ResolveGlyphs(asciiSetting string, ambiwidth int, getenv func(string) string) Glyphs {
	switch asciiSetting {
	case "on":
		return GlyphsASCII
	case "off":
		return GlyphsUnicode
	}
	if ambiwidth == 2 || getenv("TERM") == "linux" {
		return GlyphsASCII
	}
	if runtime.GOOS != "windows" {
		locale := ""
		for _, v := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
			if locale = getenv(v); locale != "" {
				break
			}
		}
		l := strings.ToLower(locale)
		if !strings.Contains(l, "utf-8") && !strings.Contains(l, "utf8") {
			return GlyphsASCII
		}
	}
	return GlyphsUnicode
}

// SetAmbiWidth makes East Asian ambiguous characters 1 or 2 columns wide
// in all width computations.
func SetAmbiWidth(n int) {
	runewidth.DefaultCondition.EastAsianWidth = n == 2
}
