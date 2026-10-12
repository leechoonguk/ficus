package engine

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ZshHighlights converts the menu into the POSTDISPLAY text and
// region_highlight specs ("start end attrs") with offsets relative to the
// start of POSTDISPLAY, in characters as zsh counts them. The ghost comes
// first, on the line itself.
func (m Menu) ZshHighlights() (text string, specs []string) {
	var b strings.Builder
	off := utf8.RuneCountInString(m.Ghost.Text)
	b.WriteString(m.Ghost.Text)
	if attrs := m.Ghost.Style.zsh(); off > 0 && attrs != "" {
		specs = append(specs, fmt.Sprintf("0 %d %s", off, attrs))
	}
	for _, line := range m.Lines {
		b.WriteByte('\n')
		off++
		for _, sp := range line {
			n := utf8.RuneCountInString(sp.Text)
			if attrs := sp.Style.zsh(); attrs != "" {
				specs = append(specs, fmt.Sprintf("%d %d %s", off, off+n, attrs))
			}
			b.WriteString(sp.Text)
			off += n
		}
	}
	return b.String(), specs
}

func (s Style) zsh() string {
	var a []string
	if s.Fg >= 0 {
		a = append(a, "fg="+strconv.Itoa(s.Fg))
	}
	if s.Bg >= 0 {
		a = append(a, "bg="+strconv.Itoa(s.Bg))
	}
	if s.Bold {
		a = append(a, "bold")
	}
	if s.Underline {
		a = append(a, "underline")
	}
	if s.Reverse {
		a = append(a, "standout")
	}
	return strings.Join(a, ",")
}

// ANSI renders the menu lines with SGR escapes, each line ending in a reset.
func (m Menu) ANSI() []string {
	out := make([]string, 0, len(m.Lines))
	for _, line := range m.Lines {
		var b strings.Builder
		for _, sp := range line {
			b.WriteString(sp.Style.SGR())
			b.WriteString(sp.Text)
			b.WriteString("\x1b[0m")
		}
		out = append(out, b.String())
	}
	return out
}

// SGR uses the 8/16-color codes for palette colors so that terminals
// without 256 colors understand them.
func (s Style) SGR() string {
	var a []string
	if s.Bold {
		a = append(a, "1")
	}
	if s.Underline {
		a = append(a, "4")
	}
	if s.Reverse {
		a = append(a, "7")
	}
	switch {
	case s.Fg >= 16:
		a = append(a, "38;5;"+strconv.Itoa(s.Fg))
	case s.Fg >= 8:
		a = append(a, strconv.Itoa(90+s.Fg-8))
	case s.Fg >= 0:
		a = append(a, strconv.Itoa(30+s.Fg))
	}
	switch {
	case s.Bg >= 16:
		a = append(a, "48;5;"+strconv.Itoa(s.Bg))
	case s.Bg >= 8:
		a = append(a, strconv.Itoa(100+s.Bg-8))
	case s.Bg >= 0:
		a = append(a, strconv.Itoa(40+s.Bg))
	}
	if len(a) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(a, ";") + "m"
}

// QuotePosix quotes s as a $'...' word, understood by zsh and bash. Bytes
// that are not valid UTF-8 are kept as \xHH escapes.
func QuotePosix(s string) string {
	var b strings.Builder
	b.WriteString("$'")
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\'':
			b.WriteString(`\'`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == 0x1b:
			b.WriteString(`\e`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('\'')
	return b.String()
}
