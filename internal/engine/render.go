package engine

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

func init() {
	// go-runewidth derives East Asian Width from the locale (e.g. ko_KR makes
	// "…" two columns wide); terminals and libc wcwidth render ambiguous
	// characters one column wide unless configured otherwise (SetAmbiWidth).
	runewidth.DefaultCondition.EastAsianWidth = false
}

// Span is a run of equally styled text.
type Span struct {
	Text  string
	Style Style
}

// Menu is a rendered completion menu.
type Menu struct {
	Lines [][]Span
	Count int // number of matches
	Sel   int // selected match after wrap-around
	// Ghost is the rest of the line Tab would insert, drawn after the
	// cursor (zsh, pwsh).
	Ghost Span
}

// Layout bounds the menu.
type Layout struct {
	Width   int // terminal columns
	Height  int // terminal lines
	MaxRows int // most item rows shown at once (the rows setting)
	Details int // most lines of the description pane
}

// RenderOptions control how Render draws the menu.
type RenderOptions struct {
	Sel      int
	Selected bool
	Layout   Layout
	Theme    Theme
	Glyphs   Glyphs
	Icons    string // none | ascii | nerd: the item kind column
	Groups   bool   // group items by tag under headers (see Grouped)
	Pattern  string // the typed filter; groups only show without one
}

// rowBudget returns how many of total display rows fit next to extra
// lines (the details pane) and, when status or when not all rows fit, the
// status line: a short terminal keeps the menu small so the line being
// edited stays in view. 0 means none fit.
func rowBudget(total int, lay Layout, extra int, status bool) int {
	if lay.Width < 20 || lay.Height < 5 {
		return 0
	}
	limit := 50
	switch {
	case lay.Height <= 10:
		limit = 3
	case lay.Height <= 20:
		limit = 5
	}
	avail := lay.Height - 2 - extra
	rows := min(total, max(lay.MaxRows, 1), limit, avail)
	if rows < total || status {
		rows = min(rows, avail-1)
	}
	return max(rows, 0)
}

// PageRows is how many items a page of the menu shows for n matches.
func (l Layout) PageRows(n int) int {
	return max(rowBudget(n, l, 0, false), 1)
}

var kindGlyphs = map[ItemKind][2]string{
	ItemCmd:     {">", "\uf120"},
	ItemAlias:   {"@", "\uf0c1"},
	ItemFunc:    {"f", "\uf121"},
	ItemBuiltin: {"b", "\uf013"},
	ItemFlag:    {"-", "\uf024"},
	ItemDir:     {"/", "\uf07b"},
	ItemFile:    {".", "\uf15b"},
	ItemVar:     {"$", "\uf155"},
}

func kindGlyph(k ItemKind, icons string) string {
	g, ok := kindGlyphs[k]
	switch {
	case !ok:
		return " "
	case icons == "nerd":
		return g[1]
	}
	return g[0]
}

// display is one menu row: a group header or an item.
type display struct {
	header string
	item   int // index into matches; -1 for a header
}

// Render lays out matches as rows of "label  description". With selected,
// row sel is highlighted and marked with ">"; sel wraps around in both
// directions so the shell can step it blindly. Without, nothing is
// highlighted: the menu only shows what Tab or Up/Down would take, and Enter
// runs the line as typed. Grouped matches (see GroupOrder) get a header per
// tag; a description too long for its row is wrapped below the rows.
func Render(matches []Match, usage string, opts RenderOptions) Menu {
	lay, th, gl := opts.Layout, opts.Theme, opts.Glyphs
	if lay.Width < 20 || lay.Height < 5 {
		return Menu{}
	}
	width := lay.Width - 1 // never touch the last column: avoids autowrap
	n := len(matches)
	if n == 0 {
		if usage == "" {
			return Menu{}
		}
		return Menu{Lines: [][]Span{{{Text: " " + truncate(usage, width-2, gl.Ellipsis) + " ", Style: th.Status}}}}
	}
	sel := ((opts.Sel % n) + n) % n
	selected := opts.Selected
	target := 0 // the item the details pane and status describe
	if selected {
		target = sel
	}

	grouped := Grouped(matches, opts.Groups, opts.Pattern)
	rows := make([]display, 0, n)
	selRow := 0
	for i, m := range matches {
		if grouped && m.Tag != "" && (i == 0 || m.Tag != matches[i-1].Tag) {
			rows = append(rows, display{header: m.Tag, item: -1})
		}
		if i == sel {
			selRow = len(rows)
		}
		rows = append(rows, display{item: i})
	}

	icons := false
	if opts.Icons != "none" && opts.Icons != "" {
		for _, m := range matches {
			if m.Kind != "" {
				icons = true
				break
			}
		}
	}
	kindW := 0
	if icons {
		kindW = 2
	}
	labelW, descW := 1, 0
	for _, m := range matches {
		labelW = max(labelW, runewidth.StringWidth(m.Label()))
		descW = max(descW, runewidth.StringWidth(m.Description))
	}
	labelW = min(labelW, max(12, (width-kindW)*45/100), width-kindW-2)
	// A column too narrow for its text is dropped (the details pane shows
	// the selected description instead); short descriptions keep theirs.
	if fit := min(descW, width-kindW-labelW-4); fit < min(8, descW) {
		descW = 0
	} else {
		descW = fit
	}

	details := detailLines(matches[target].Description, descW, lay, gl)
	visible := rowBudget(len(rows), lay, len(details), grouped)
	if visible < 1 && len(details) > 0 {
		details = nil
		visible = rowBudget(len(rows), lay, 0, grouped)
	}
	if visible < 1 {
		return Menu{}
	}
	start := 0
	if selected && selRow >= visible {
		start = selRow - visible + 1
	}

	menu := Menu{Count: n, Sel: sel}
	shown := 0
	for _, r := range rows[start : start+visible] {
		if r.item < 0 {
			text := " " + gl.Rule + gl.Rule + " " + r.header
			menu.Lines = append(menu.Lines, []Span{{Text: truncate(text, width, gl.Ellipsis), Style: th.Header}})
			continue
		}
		shown++
		m := matches[r.item]
		label, desc, match, kind := th.Label, th.Desc, th.Match, th.Kind
		marker := " "
		if selected && r.item == sel {
			label, desc = th.Selected, th.SelectedDesc
			match.Bg, match.Reverse = th.Selected.Bg, th.Selected.Reverse
			kind.Bg, kind.Reverse = th.Selected.Bg, th.Selected.Reverse
			marker = ">"
		}
		line := []Span{{Text: marker, Style: label}}
		if icons {
			line = append(line, Span{Text: kindGlyph(m.Kind, opts.Icons) + " ", Style: kind})
		}
		line = append(line, highlightSpans(m.Label(), m.Positions, labelW, label, match, gl.Ellipsis)...)
		if descW > 0 {
			line = append(line, Span{Text: "  " + pad(truncate(m.Description, descW, gl.Ellipsis), descW) + " ", Style: desc})
		} else {
			line = append(line, Span{Text: " ", Style: label})
		}
		menu.Lines = append(menu.Lines, line)
	}
	for _, d := range details {
		menu.Lines = append(menu.Lines, []Span{{Text: "   " + d, Style: th.Desc}})
	}
	if n > shown || grouped {
		pos := "-"
		if selected {
			pos = fmt.Sprint(sel + 1)
		}
		status := fmt.Sprintf(" %s/%d", pos, n)
		if grouped && matches[target].Tag != "" {
			status += " " + gl.Sep + " " + matches[target].Tag
		}
		menu.Lines = append(menu.Lines, []Span{{Text: truncate(status, width, gl.Ellipsis), Style: th.Status}})
	}
	return menu
}

// detailLines wraps a description that does not fit its column (descW) to
// the menu width, at most lay.Details lines; the last one ends in an
// ellipsis when text is left over. Wrapping is per character, so text
// without spaces (CJK) wraps too.
func detailLines(desc string, descW int, lay Layout, gl Glyphs) []string {
	desc = strings.TrimSpace(Printable(desc))
	if lay.Details <= 0 || desc == "" || (descW > 0 && runewidth.StringWidth(desc) <= descW) {
		return nil
	}
	w := lay.Width - 4
	if w < 8 {
		return nil
	}
	var lines []string
	rest := desc
	for rest != "" && len(lines) < lay.Details {
		if len(lines) == lay.Details-1 {
			lines = append(lines, truncate(rest, w, gl.Ellipsis))
			break
		}
		col, cut := 0, len(rest)
		for i, r := range rest {
			rw := runewidth.RuneWidth(r)
			if col+rw > w {
				cut = i
				break
			}
			col += rw
		}
		lines = append(lines, rest[:cut])
		rest = strings.TrimLeft(rest[cut:], " ")
	}
	return lines
}

// Grouped reports whether the menu groups matches by tag: asked for, no
// filter typed (a ranked list must stay in rank order) and two tags or more.
func Grouped(matches []Match, groups bool, pattern string) bool {
	if !groups || pattern != "" {
		return false
	}
	first := ""
	for _, m := range matches {
		switch {
		case m.Tag == "":
		case first == "":
			first = m.Tag
		case m.Tag != first:
			return true
		}
	}
	return false
}

// GroupOrder puts matches of the same tag together, groups in the order
// their tags first appear, when the menu is grouped. Every command that
// indexes the menu (complete, accept, --move) must use this order.
func GroupOrder(matches []Match, groups bool, pattern string) []Match {
	if !Grouped(matches, groups, pattern) {
		return matches
	}
	rank := map[string]int{}
	for _, m := range matches {
		if _, ok := rank[m.Tag]; !ok {
			rank[m.Tag] = len(rank)
		}
	}
	out := slices.Clone(matches)
	slices.SortStableFunc(out, func(a, b Match) int { return rank[a.Tag] - rank[b.Tag] })
	return out
}

// MoveGroup returns the selection after jumping to the next (dir > 0) or
// previous group: the first item of a group. Ungrouped, it moves a page
// (page items) and wraps only from either end.
func MoveGroup(matches []Match, grouped bool, sel int, selected bool, dir, page int) int {
	n := len(matches)
	if n == 0 {
		return 0
	}
	sel = ((sel % n) + n) % n
	if !grouped {
		switch {
		case !selected && dir > 0:
			return 0
		case !selected:
			return n - 1
		case dir > 0 && sel == n-1:
			return 0
		case dir > 0:
			return min(sel+page, n-1)
		case sel == 0:
			return n - 1
		default:
			return max(sel-page, 0)
		}
	}
	var starts []int
	for i, m := range matches {
		if i == 0 || m.Tag != matches[i-1].Tag {
			starts = append(starts, i)
		}
	}
	cur := 0 // the group sel is in
	for g, s := range starts {
		if s <= sel {
			cur = g
		}
	}
	switch {
	case !selected && dir > 0:
		return 0
	case !selected:
		return starts[len(starts)-1]
	case dir > 0:
		return starts[(cur+1)%len(starts)]
	case sel != starts[cur]:
		return starts[cur]
	default:
		return starts[(cur-1+len(starts))%len(starts)]
	}
}

// ErrorMenu shows a backend failure in place of the menu.
func ErrorMenu(err error, opts RenderOptions) Menu {
	lay := opts.Layout
	if lay.Width < 20 || lay.Height < 5 {
		return Menu{}
	}
	return Menu{Lines: [][]Span{{{Text: " ficus: " + truncate(err.Error(), lay.Width-10, opts.Glyphs.Ellipsis), Style: opts.Theme.Error}}}}
}

// StatusMenu shows a one-line notice (e.g. candidates still loading).
func StatusMenu(text string, opts RenderOptions) Menu {
	lay := opts.Layout
	if lay.Width < 20 || lay.Height < 5 {
		return Menu{}
	}
	return Menu{Lines: [][]Span{{{Text: truncate(text, lay.Width-1, opts.Glyphs.Ellipsis), Style: opts.Theme.Status}}}}
}

// highlightSpans fits label into w columns and styles the matched runes.
func highlightSpans(label string, positions []int, w int, base, match Style, ellipsis string) []Span {
	shown := truncate(label, w, ellipsis)
	text := pad(shown, w)
	hit := make(map[int]bool, len(positions))
	for _, p := range positions {
		hit[p] = true
	}
	var spans []Span
	var cur strings.Builder
	curHit := false
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		st := base
		if curHit {
			st = match
		}
		spans = append(spans, Span{Text: cur.String(), Style: st})
		cur.Reset()
	}
	limit := utf8.RuneCountInString(shown)
	if shown != Printable(label) && strings.HasSuffix(shown, ellipsis) {
		limit -= utf8.RuneCountInString(ellipsis) // the ellipsis replaced label runes
	}
	for i, r := range []rune(text) {
		h := hit[i] && i < limit
		if h != curHit {
			flush()
			curHit = h
		}
		cur.WriteRune(r)
	}
	flush()
	return spans
}

// Printable replaces control characters (C0, DEL and C1, which terminals
// may act on) with spaces, one for one so highlight positions stay valid.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, s)
}

// EndRow returns the screen row (0 = the prompt's last line) on which the
// cursor sits after the prompt's last line and line are printed on a
// cols-wide terminal: wide characters take two columns and wrap whole, an
// embedded newline starts a new row at column 0, and a row filled exactly
// moves the cursor to the next one, as readline does.
func EndRow(prompt, line string, cols int) int {
	if cols < 1 {
		cols = 80
	}
	row, col := 0, 0
	for _, r := range prompt + line {
		if r == '\n' {
			row, col = row+1, 0
			continue
		}
		w := runewidth.RuneWidth(r)
		if col+w > cols {
			row, col = row+1, 0
		}
		col += w
		if col == cols {
			row, col = row+1, 0
		}
	}
	return row
}

// truncate fits s into w columns, ending in ellipsis when it is cut.
func truncate(s string, w int, ellipsis string) string {
	s = Printable(s)
	if runewidth.StringWidth(s) <= w {
		return s
	}
	if w <= runewidth.StringWidth(ellipsis) {
		return runewidth.Truncate(s, w, "")
	}
	return runewidth.Truncate(s, w, ellipsis)
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-runewidth.StringWidth(s)))
}
