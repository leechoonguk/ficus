// Command ficus is an in-terminal, as-you-type completion menu for zsh, bash
// and PowerShell. Shell integrations (ficus init <shell>) call it on every
// edit; see README.md for the protocol.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ficus/internal/config"
	"ficus/internal/engine"
	"ficus/internal/setup"
	"ficus/internal/termq"
	"ficus/shell"

	fzf "github.com/junegunn/fzf/src"
	"github.com/mattn/go-runewidth"
)

var version = "develop"

const usage = `usage:
  ficus init <zsh|bash|pwsh>     print the shell integration
  ficus complete --shell S [--sel N] [--unselected] [--manual] [--move next-group|prev-group]
                 [--width W] [--height H] [--colors N] [--theme dark|light|ansi] [--budget D]
                 [--ghost] [--prompt P] [--external] -- LBUFFER
  ficus accept   --shell S [--sel N] [--unselected] [--external] -- LBUFFER
  ficus pick     --shell S [--fullscreen] [--external] -- LBUFFER   fuzzy picker (fzf)
  ficus setup [--shells zsh,bash,pwsh,powershell] [--remove]
                                 load ficus in every new shell (startup file blocks), or undo it
  ficus doctor                   show what ficus detects about the terminal and settings
  ficus update [--check] [--force]
                                 install the latest release over this ficus and ficus-carapace
  ficus version

environment:
  FICUS_CARAPACE        path of ficus-carapace (default: next to ficus, then PATH)
  FICUS_CACHE_DIR       candidate cache (default: <user cache dir>/ficus)
  FICUS_CONFIG          settings file (default: ~/.config/ficus/config.toml); FICUS_<KEY> overrides a key
  FICUS_EXTRA_COMMANDS  file of "name<TAB>description" lines for command position
  FICUS_LBUFFER         the line left of the cursor, when no LBUFFER argument is given
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ficus:", err)
		os.Exit(2)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("missing subcommand")
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(out, "ficus", version)
		return nil
	case "help", "--help", "-h":
		fmt.Fprint(out, usage)
		return nil
	case "init":
		if len(args) != 2 {
			return errors.New("usage: ficus init <zsh|bash|pwsh>")
		}
		sh, err := engine.ParseShell(args[1])
		if err != nil {
			return err
		}
		s, _, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ficus:", err)
		}
		script, err := shell.Script(string(sh), s, initTheme(s.Theme))
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, script)
		return err
	case "doctor":
		return doctor(out)
	case "setup":
		return runSetup(args[1:], out)
	case "update":
		return runUpdate(args[1:], out)
	case "complete", "accept", "pick", "fill":
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}

	s, _, _ := config.Load() // init has reported any problem
	defTheme := s.Theme
	if defTheme == "auto" {
		defTheme = "ansi" // init resolves auto; a direct call has no terminal to ask
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	shellName := fs.String("shell", "", "zsh, bash or pwsh")
	sel := fs.Int("sel", 0, "selected match index")
	width := fs.Int("width", 80, "terminal width")
	height := fs.Int("height", 24, "terminal height in lines")
	colors := fs.Int("colors", 256, "colors the terminal supports (terminfo colors)")
	themeName := fs.String("theme", defTheme, "dark, light or ansi")
	external := fs.Bool("external", false, `read candidates from stdin ("value<TAB>label<TAB>description" lines) instead of fetching`)
	unselected := fs.Bool("unselected", false, "no item chosen with Up/Down yet. complete: highlight the first item (preselect), the run row (engine.RunAsTyped) or none; accept: insert what all matches share, if more than typed")
	fullscreen := fs.Bool("fullscreen", false, "pick: use the whole screen instead of an inline window")
	prompt := fs.String("prompt", "", "complete (bash): visible text of the prompt's last line, to place the menu")
	budget := fs.Duration("budget", s.Budget, "longest wait for candidates not yet cached; 0 waits for the completer")
	manual := fs.Bool("manual", false, "complete: the user asked for the menu (Tab): show it even for a short command word")
	move := fs.String("move", "", "complete: next-group or prev-group: select the first item of the next/previous group")
	ghost := fs.Bool("ghost", false, "complete: also print the rest of the line Tab would insert, to show after the cursor")
	result := fs.String("result", "", "pick: write the result to this file (inside a tmux popup)")
	externalFile := fs.String("external-file", "", "pick: read --external candidates from this file")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	sh, err := engine.ParseShell(*shellName)
	if err != nil {
		return err
	}
	lbuffer := strings.Join(fs.Args(), " ")
	if fs.NArg() == 0 {
		// PowerShell passes the line this way: its Windows-mode native
		// argument passing mangles embedded quotes.
		lbuffer = os.Getenv("FICUS_LBUFFER")
	}

	cfg := engineConfig(s)
	engine.SetAmbiWidth(s.AmbiWidth)
	ctx := engine.Parse(sh, lbuffer)
	if args[0] == "fill" {
		// Started by Spawn below: fill the cache for the line in the
		// background while the shell shows "loading".
		return engine.Fill(cfg, ctx)
	}
	cfg.Budget = *budget
	cfg.Spawn = func() error { return spawnFill(sh, lbuffer) }
	var externalTSV []byte
	if *externalFile != "" {
		if externalTSV, err = os.ReadFile(*externalFile); err != nil {
			return err
		}
	} else if *external {
		// The shell's own completer (PowerShell's TabExpansion2 for cmdlets).
		// Through stdin: environment blocks are size-limited (32 KiB on Windows).
		if externalTSV, err = io.ReadAll(os.Stdin); err != nil {
			return err
		}
	}
	if args[0] == "pick" && *result == "" && popupAvailable(s) {
		done, err := pickInPopup(out, sh, lbuffer, externalTSV, *external || *externalFile != "")
		if done || err != nil {
			return err
		}
		// The popup failed to open: fall back to the inline picker.
	}
	var cands *engine.Candidates
	var fetchErr error
	if *external || *externalFile != "" {
		cands = engine.ParseExternal(string(externalTSV))
	} else if cands, fetchErr = engine.Fetch(cfg, ctx); fetchErr != nil {
		cands = &engine.Candidates{}
	}
	var recent map[string]int
	if s.Recent {
		recent = engine.LoadRecent(cfg, ctx)
	}
	// Grouped menus list each tag's items together; accept must index the
	// same order the menu shows.
	matches := engine.GroupOrder(engine.Filter(cands.Items, ctx.FetchPrefix, ctx.Pattern, recent), s.Groups, ctx.Pattern)

	switch args[0] {
	case "complete":
		theme, ghostOK := engine.ResolveTheme(*themeName, s.Color, *colors, os.Getenv)
		glyphs := engine.ResolveGlyphs(s.ASCII, s.AmbiWidth, os.Getenv)
		icons := s.Icons
		if icons == "nerd" && glyphs == engine.GlyphsASCII {
			icons = "ascii" // Nerd Font icons are not ASCII either
		}
		opts := engine.RenderOptions{
			Sel:      *sel,
			Selected: !*unselected,
			Layout:   engine.Layout{Width: *width, Height: *height, MaxRows: s.Rows, Details: s.Details},
			Theme:    theme,
			Glyphs:   glyphs,
			Icons:    icons,
			Groups:   s.Groups,
			Pattern:  ctx.Pattern,
		}
		// Until Up/Down chooses, the first item is highlighted: Enter
		// completes with it (and the shell keeps the menu closed after, so
		// the next Enter runs the line). When what is typed is already the
		// line to run (engine.RunAsTyped: an empty word, `.`/`..`, a word
		// that names a candidate, only fuzzy matches), a "run" row on top is
		// highlighted instead and Enter runs the line, as in Fig.
		opts.RunRow = s.Preselect && fetchErr == nil && engine.RunAsTyped(matches, ctx)
		presel := *unselected && s.Preselect && fetchErr == nil && !opts.RunRow
		if presel {
			opts.Sel, opts.Selected = 0, true
		}
		switch *move {
		case "":
		case "next-group", "prev-group":
			dir := 1
			if *move == "prev-group" {
				dir = -1
			}
			grouped := engine.Grouped(matches, s.Groups, ctx.Pattern)
			opts.Sel = engine.MoveGroup(matches, grouped, *sel, opts.Selected, dir, opts.Layout.PageRows(len(matches)))
			opts.Selected = true
		default:
			return fmt.Errorf("--move %q: want next-group or prev-group", *move)
		}
		menu := engine.Render(matches, cands.Usage, opts)
		switch {
		case errors.Is(fetchErr, engine.ErrLoading):
			menu = engine.StatusMenu(" loading"+opts.Glyphs.Ellipsis, opts)
		case fetchErr != nil:
			menu = engine.ErrorMenu(fetchErr, opts)
		}
		if exactOnly(matches, ctx) {
			menu = engine.Menu{} // the word is already complete
		}
		held := false
		if ctx.Kind == engine.KindCommand && !*manual && holdCommandMenu(matches, ctx, s.MinInput) {
			// Every first letter would flash hundreds of commands: wait for
			// min_input characters, and hide the menu once the word names a
			// command. Tab still opens it (held).
			menu, held = engine.Menu{}, len(matches) > 0
		}
		if slices.Contains(s.DisableFor, ctx.Words[0]) {
			menu, held = engine.Menu{}, false // disable_for: no menu for this command
		}
		if *ghost && ghostOK && menu.Count > 0 && !(opts.RunRow && *unselected) {
			// The rest of the line accepting the highlighted item gives,
			// when it only appends to what is typed. None while the run
			// row is highlighted: Enter adds nothing then.
			newL := ctx.Accept(cands, matches[menu.Sel].Value)
			if strings.HasPrefix(newL, lbuffer) {
				menu.Ghost = engine.Span{Text: engine.Printable(strings.TrimSuffix(newL[len(lbuffer):], " ")), Style: theme.Ghost}
			}
		}
		return writeMenu(out, sh, menu, engine.EndRow(*prompt, lbuffer, *width), held, presel && menu.Count > 0)
	case "accept":
		if fetchErr != nil {
			return fetchErr
		}
		if len(matches) == 0 {
			return errors.New("no match")
		}
		if *unselected && !cands.Verbatim {
			// Nothing chosen yet: Tab first inserts what every match shares.
			if lcp, ok := engine.CommonPrefix(matches, ctx.FetchPrefix, ctx.Pattern); ok {
				return writeBuffer(out, sh, ctx.AcceptPartial(ctx.FetchPrefix+lcp))
			}
		}
		m := matches[((*sel%len(matches))+len(matches))%len(matches)]
		if s.Recent {
			_ = engine.RecordRecent(cfg, ctx, m.Value) // a ranking hint only
		}
		return writeBuffer(out, sh, ctx.Accept(cands, m.Value))
	default: // pick
		if fetchErr != nil {
			return fetchErr
		}
		preview := s.PickerPreview && runtime.GOOS != "windows" && slices.ContainsFunc(cands.Items, func(it engine.Item) bool {
			return it.Kind == engine.ItemFile || it.Kind == engine.ItemDir
		})
		m, ok, err := pick(cands.Items, ctx.Pattern, *fullscreen, preview)
		if err != nil || !ok {
			return err
		}
		if *result != "" {
			var b strings.Builder
			if err := writeBuffer(&b, sh, ctx.Accept(cands, m.Value)); err != nil {
				return err
			}
			return os.WriteFile(*result, []byte(b.String()), 0o600)
		}
		return writeBuffer(out, sh, ctx.Accept(cands, m.Value))
	}
}

// runSetup adds (or with --remove deletes) the block that loads ficus in
// each shell's startup file. Without --shells it sets up every supported
// shell installed here.
func runSetup(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	shells := fs.String("shells", "", "comma-separated: zsh, bash, pwsh, powershell (default: every supported shell found)")
	remove := fs.Bool("remove", false, "delete the block from every startup file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("setup: unexpected argument %q", fs.Arg(0))
	}
	e := setup.System(out)
	if *remove {
		return setup.Remove(e)
	}
	list := setup.Detect(e)
	if *shells != "" {
		list = strings.Split(*shells, ",")
	}
	if len(list) == 0 {
		return errors.New("setup: no supported shell found (zsh 5.9+, bash 4+, PowerShell)")
	}
	self, err := selfPath()
	if err != nil {
		return err
	}
	if err := setup.Add(e, self, list); err != nil {
		return err
	}
	fmt.Fprintln(out, "ficus: open a new terminal to start using it")
	return nil
}

// selfPath is the path the startup files should run ficus by: the one it
// was started as (a package manager's /usr/bin or Homebrew link, which
// survives upgrades), not the resolved target of a symlink.
func selfPath() (string, error) {
	arg0 := os.Args[0]
	if strings.ContainsAny(arg0, `/\`) {
		return filepath.Abs(arg0)
	}
	if p, err := exec.LookPath(arg0); err == nil {
		return filepath.Abs(p)
	}
	return os.Executable()
}

// initTheme resolves theme "auto" once per shell: the terminal's answer
// to a background color query, else COLORFGBG ("fg;bg", set by rxvt,
// Konsole, iTerm2), else the palette-following ANSI theme.
func initTheme(name string) string {
	if name != "auto" {
		return name
	}
	if light, ok := termq.Background(150 * time.Millisecond); ok {
		if light {
			return "light"
		}
		return "dark"
	}
	return themeFromColorFGBG(os.Getenv("COLORFGBG"))
}

// themeFromColorFGBG reads the background palette index, the last field.
func themeFromColorFGBG(v string) string {
	if v == "" {
		return "ansi"
	}
	f := strings.Split(v, ";")
	bg, err := strconv.Atoi(f[len(f)-1])
	switch {
	case err != nil || bg < 0 || bg > 15:
		return "ansi"
	case bg <= 6 || bg == 8:
		return "dark"
	default:
		return "light"
	}
}

// exactOnly reports a single match equal to what is typed: showing it would
// only repeat the word.
func exactOnly(matches []engine.Match, ctx *engine.Context) bool {
	return len(matches) == 1 && ctx.Pattern != "" &&
		strings.TrimPrefix(matches[0].Value, ctx.FetchPrefix) == ctx.Pattern
}

// holdCommandMenu reports a command word shorter than minInput, or one
// that already names a command.
func holdCommandMenu(matches []engine.Match, ctx *engine.Context, minInput int) bool {
	return utf8.RuneCountInString(ctx.Pattern) < minInput || namesCandidate(matches, ctx)
}

// namesCandidate reports a typed word that is already one of the matches.
func namesCandidate(matches []engine.Match, ctx *engine.Context) bool {
	for _, m := range matches {
		if strings.TrimPrefix(m.Value, ctx.FetchPrefix) == ctx.Pattern {
			return true
		}
	}
	return false
}

// spawnFill starts `ficus fill` for the line in the background, detached
// from the shell, and does not wait for it.
func spawnFill(sh engine.Shell, lbuffer string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "fill", "--shell", string(sh))
	cmd.Env = append(os.Environ(), "FICUS_LBUFFER="+lbuffer)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func engineConfig(s config.Settings) engine.Config {
	cfg := engine.Config{
		Carapace:      os.Getenv("FICUS_CARAPACE"),
		CacheDir:      os.Getenv("FICUS_CACHE_DIR"),
		TTL:           s.TTL,
		Timeout:       3 * time.Second,
		ExtraCommands: os.Getenv("FICUS_EXTRA_COMMANDS"),
		HelpFlags:     s.HelpFlags,
		Bridges:       strings.Join(s.Bridges, ","),
	}
	if cfg.CacheDir == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			cfg.CacheDir = filepath.Join(dir, "ficus")
		}
	}
	if cfg.Carapace == "" {
		name := "ficus-carapace"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cfg.Carapace = name
		if self, err := os.Executable(); err == nil {
			if p := filepath.Join(filepath.Dir(self), name); fileExists(p) {
				cfg.Carapace = p
			}
		}
		if !filepath.IsAbs(cfg.Carapace) {
			if p, err := exec.LookPath(name); err == nil {
				cfg.Carapace = p
			}
		}
	}
	return cfg
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// writeMenu prints the menu for the shell integration.
//
// zsh: assignments of __ficus_n, __ficus_sel, __ficus_held (menu hidden
// until Tab), __ficus_presel (sel is highlighted though Up/Down did not
// choose it: Enter completes with it), __ficus_post (POSTDISPLAY text) and
// __ficus_hl (region_highlight specs relative to POSTDISPLAY).
// bash: assignments of __ficus_n, __ficus_sel, __ficus_held,
// __ficus_presel, __ficus_rows, __ficus_endrow (row the line ends on, given
// --prompt) and __ficus_menu (ANSI lines joined by newlines).
// zsh also gets __ficus_ghost, the length of the ghost text that starts
// __ficus_post.
// pwsh: first line "n sel held presel<TAB>ghost" (ghost with its ANSI
// style), then one ANSI line per menu row.
func writeMenu(out io.Writer, sh engine.Shell, m engine.Menu, endRow int, held, presel bool) error {
	ghost := utf8.RuneCountInString(m.Ghost.Text)
	h, p := bit(held), bit(presel)
	var b strings.Builder
	switch sh {
	case engine.Zsh:
		text, specs := m.ZshHighlights()
		fmt.Fprintf(&b, "__ficus_n=%d __ficus_sel=%d __ficus_held=%d __ficus_presel=%d __ficus_ghost=%d\n__ficus_post=%s\n__ficus_hl=(", m.Count, m.Sel, h, p, ghost, engine.QuotePosix(text))
		for _, s := range specs {
			b.WriteString(" " + engine.QuotePosix(s))
		}
		b.WriteString(" )\n")
	case engine.Bash:
		lines := m.ANSI()
		fmt.Fprintf(&b, "__ficus_n=%d __ficus_sel=%d __ficus_held=%d __ficus_presel=%d __ficus_rows=%d __ficus_endrow=%d\n__ficus_menu=%s\n",
			m.Count, m.Sel, h, p, len(lines), endRow, engine.QuotePosix(strings.Join(lines, "\n")))
	default:
		g := ""
		if ghost > 0 {
			g = m.Ghost.Style.SGR() + m.Ghost.Text + "\x1b[0m"
		}
		fmt.Fprintf(&b, "%d %d %d %d\t%s\n", m.Count, m.Sel, h, p, g)
		for _, l := range m.ANSI() {
			b.WriteString(l + "\n")
		}
	}
	_, err := io.WriteString(out, b.String())
	return err
}

func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}

// writeBuffer prints the new left buffer: an assignment for zsh/bash, raw
// text for pwsh.
func writeBuffer(out io.Writer, sh engine.Shell, lbuffer string) error {
	if sh == engine.Powershell {
		_, err := io.WriteString(out, lbuffer)
		return err
	}
	_, err := fmt.Fprintf(out, "__ficus_lbuffer=%s\n", engine.QuotePosix(lbuffer))
	return err
}

// pick runs fzf in-process over all candidates, seeded with the typed
// pattern: the fzf-tab experience in every shell. The inline (--height)
// window asks the terminal for the cursor position; PowerShell's PSReadLine
// swallows that reply, so it needs fullscreen.
//
// FZF_DEFAULT_OPTS is ignored: options such as --multi, --print-query or
// --tmux change what fzf outputs or re-execute this process.
func pick(items []engine.Item, query string, fullscreen, preview bool) (engine.Item, bool, error) {
	if len(items) == 0 {
		return engine.Item{}, false, nil
	}
	args := []string{
		"--layout=reverse", "--border=rounded", "--info=inline-right",
		"--delimiter=\t", "--with-nth=2,3", "--nth=1", "--ansi", "--tiebreak=begin",
		"--query=" + query, "--select-1", "--exit-0",
	}
	if !fullscreen {
		args = append(args, "--height=~50%")
	}
	if preview {
		// Field 4 is the value: list a directory, show the start of a file.
		args = append(args, "--with-shell=sh -c",
			`--preview=p={4}; case $p in "~/"*) p="$HOME/${p#\~/}";; esac; if [ -d "$p" ]; then ls -A -- "$p"; elif [ -f "$p" ]; then head -c 20000 -- "$p"; fi`,
			"--preview-window=right,50%")
	}
	opts, err := fzf.ParseOptions(false, args)
	if err != nil {
		return engine.Item{}, false, err
	}
	in := make(chan string)
	outc := make(chan string)
	opts.Input, opts.Output = in, outc
	var selected []string
	collected := make(chan struct{})
	go func() {
		for line := range outc {
			selected = append(selected, line)
		}
		close(collected)
	}()

	labelW := 0
	for _, it := range items {
		labelW = max(labelW, runewidth.StringWidth(engine.Printable(it.Label())))
	}
	labelW = min(labelW, 40)
	go func() {
		for i, it := range items {
			// --ansi is on for the description color: candidate text must not
			// carry escape sequences of its own.
			label := engine.Printable(it.Label())
			label += strings.Repeat(" ", max(0, labelW-runewidth.StringWidth(label)))
			desc := ""
			if it.Description != "" {
				desc = " \x1b[38;5;245m" + engine.Printable(it.Description) + "\x1b[0m"
			}
			in <- strconv.Itoa(i) + "\t" + label + "\t" + desc + "\t" + engine.Printable(it.Value)
		}
		close(in)
	}()

	code, err := fzf.Run(opts)
	close(outc)
	<-collected
	if err != nil {
		return engine.Item{}, false, err
	}
	if code != fzf.ExitOk || len(selected) == 0 {
		return engine.Item{}, false, nil
	}
	idx, _, _ := strings.Cut(selected[0], "\t")
	i, err := strconv.Atoi(idx)
	if err != nil || i < 0 || i >= len(items) {
		return engine.Item{}, false, fmt.Errorf("unexpected fzf output %q", selected[0])
	}
	return items[i], true, nil
}
