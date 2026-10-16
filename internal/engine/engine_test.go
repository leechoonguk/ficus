package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

func TestParseSplitsFetchPrefix(t *testing.T) {
	tests := []struct {
		shell         Shell
		line          string
		kind          Kind
		words         []string
		fetch, filter string
	}{
		{Zsh, "", KindCommand, []string{""}, "", ""},
		{Zsh, "gi", KindCommand, []string{"gi"}, "", "gi"},
		{Zsh, "git ", KindArgument, []string{"git", ""}, "", ""},
		{Zsh, "git ch", KindArgument, []string{"git", "ch"}, "", "ch"},
		// Completers only list flags for a dashed word: keep the dashes.
		{Zsh, "git commit --am", KindArgument, []string{"git", "commit", "--am"}, "--", "am"},
		{Zsh, "ls -l", KindArgument, []string{"ls", "-l"}, "-", "l"},
		{Zsh, "ls src/fo", KindArgument, []string{"ls", "src/fo"}, "src/", "fo"},
		{Zsh, "git log --format=on", KindArgument, []string{"git", "log", "--format=on"}, "--format=", "on"},
		{Zsh, "./scr", KindFile, []string{"./scr"}, "./", "scr"},
		{Zsh, "cat < no", KindFile, []string{"cat"}, "", "no"},
		{Zsh, "echo $HO", KindVariable, []string{"echo", "$HO"}, "", "HO"},
		{Zsh, "ls | gr", KindCommand, []string{"gr"}, "", "gr"},
		{Bash, "x=$(git ch", KindArgument, []string{"git", "ch"}, "", "ch"},
		{Powershell, `dir C:\Win`, KindArgument, []string{"dir", `C:\Win`}, `C:\`, "Win"},
	}
	for _, tt := range tests {
		c := Parse(tt.shell, tt.line)
		if c.Kind != tt.kind || c.FetchPrefix != tt.fetch || c.Pattern != tt.filter ||
			strings.Join(c.Words, "|") != strings.Join(tt.words, "|") {
			t.Errorf("Parse(%s, %q) = kind %d words %q fetch %q pattern %q; want kind %d words %q fetch %q pattern %q",
				tt.shell, tt.line, c.Kind, c.Words, c.FetchPrefix, c.Pattern, tt.kind, tt.words, tt.fetch, tt.filter)
		}
	}
}

func TestAcceptReplacesWordWithShellQuoting(t *testing.T) {
	plain := &Candidates{Nospace: "/"}
	tests := []struct {
		shell Shell
		line  string
		cands *Candidates
		value string
		want  string
	}{
		{Zsh, "git chec", plain, "checkout", "git checkout "},
		{Zsh, "ls sr", plain, "src/", "ls src/"},                     // directory: keep typing
		{Bash, "cat my", plain, "my file.txt", `cat "my file.txt" `}, // space quoted
		{Bash, "cat 'my f", plain, "my file.txt", "cat 'my file.txt' "},
		{Zsh, "echo $HO", &Candidates{Nospace: "*"}, "HOME", "echo $HOME"},
		{Powershell, "Get-Content my", plain, "my file.txt", "Get-Content 'my file.txt' "},
		// The shell's own completer quoted it already: no second quoting.
		{Powershell, "Get-Content my", &Candidates{Verbatim: true}, "'./my file.txt'", "Get-Content './my file.txt' "},
		{Zsh, "a && git sw", plain, "switch", "a && git switch "},
	}
	for _, tt := range tests {
		if got := Parse(tt.shell, tt.line).Accept(tt.cands, tt.value); got != tt.want {
			t.Errorf("Accept(%s, %q, %q) = %q; want %q", tt.shell, tt.line, tt.value, got, tt.want)
		}
	}
}

func TestFilterRanking(t *testing.T) {
	items := []Item{{Value: "cherry"}, {Value: "rebase"}, {Value: "merge-base"}, {Value: "checkout"}, {Value: "Changelog"}}

	if got := values(Filter(items, "", "", nil)); strings.Join(got, ",") != "cherry,rebase,merge-base,checkout,Changelog" {
		t.Errorf("empty pattern must keep backend order, got %v", got)
	}
	// Prefix matches outrank better-scored fuzzy matches.
	ranked := values(Filter([]Item{{Value: "xcheck"}, {Value: "checkout"}}, "", "che", nil))
	if strings.Join(ranked, ",") != "checkout,xcheck" {
		t.Errorf("pattern che: want the prefix match first, got %v", ranked)
	}
	// Case is ignored ("REA" finds "react/", as in Fig); a match in the
	// typed case ranks first.
	if got := values(Filter(items, "", "Ch", nil)); len(got) != 3 || got[0] != "Changelog" {
		t.Errorf("pattern Ch: want Changelog first of 3, got %v", got)
	}
	if got := values(Filter(items, "", "CH", nil)); len(got) != 3 {
		t.Errorf("pattern CH must ignore case: got %v", got)
	}
	// Values carry the fetched prefix; the pattern applies to the rest.
	files := []Item{{Value: "src/main.go", Display: "main.go"}, {Value: "src/match.go", Display: "match.go"}}
	ms := Filter(files, "src/", "mtc", nil)
	if len(ms) != 1 || ms[0].Value != "src/match.go" {
		t.Fatalf("pattern mtc under src/: got %v", values(ms))
	}
	if got := ms[0].Positions; len(got) != 3 || got[0] != 0 || got[1] != 2 || got[2] != 3 {
		t.Errorf("highlight positions refer to the label: got %v", got)
	}
}

func values(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Value
	}
	return out
}

// testOpts renders with the ANSI theme and Unicode glyphs.
func testOpts(sel int, selected bool, lay Layout) RenderOptions {
	return RenderOptions{Sel: sel, Selected: selected, Layout: lay, Theme: ThemeANSI, Glyphs: GlyphsUnicode}
}

func TestRenderWindowFollowsSelection(t *testing.T) {
	var ms []Match
	for _, v := range []string{"a", "b", "c", "d", "e"} {
		ms = append(ms, Match{Item: Item{Value: v, Description: "desc " + v}})
	}
	lay := Layout{Width: 40, Height: 24, MaxRows: 3}

	m := Render(ms, "", testOpts(4, true, lay))
	if m.Sel != 4 || len(m.Lines) != 4 { // 3 rows + status
		t.Fatalf("sel 4: got sel %d, %d lines", m.Sel, len(m.Lines))
	}
	if first := lineText(m.Lines[0]); !strings.HasPrefix(first, " c") {
		t.Errorf("window must scroll so the selection is visible, first row %q", first)
	}
	if last := lineText(m.Lines[2]); !strings.HasPrefix(last, ">e") {
		t.Errorf("the selected row is marked with >, not by color alone: %q", last)
	}
	if status := lineText(m.Lines[3]); strings.TrimSpace(status) != "5/5" {
		t.Errorf("status line %q", status)
	}
	// Selection wraps both ways so the shell can step blindly.
	if Render(ms, "", testOpts(5, true, lay)).Sel != 0 || Render(ms, "", testOpts(-1, true, lay)).Sel != 4 {
		t.Error("selection does not wrap around")
	}
	// No row may reach the last column (terminals would auto-wrap it).
	long := []Match{{Item: Item{Value: strings.Repeat("x", 80), Description: strings.Repeat("한", 80)}}}
	for _, line := range Render(long, "", testOpts(0, true, lay)).Lines {
		if w := runewidth.StringWidth(lineText(line)); w >= lay.Width {
			t.Errorf("row is %d columns wide on a %d-column terminal", w, lay.Width)
		}
	}
	if m := Render(nil, "commit message", testOpts(0, false, lay)); len(m.Lines) != 1 || !strings.Contains(lineText(m.Lines[0]), "commit message") {
		t.Errorf("no matches with usage must show the usage, got %v", m.Lines)
	}
	if m := Render(nil, "", testOpts(0, false, lay)); len(m.Lines) != 0 {
		t.Errorf("no matches and no usage must render nothing, got %v", m.Lines)
	}
}

// A short terminal keeps the menu small; one too small for any gets none.
func TestRenderFitsTheTerminal(t *testing.T) {
	var ms []Match
	for i := range 20 {
		ms = append(ms, Match{Item: Item{Value: strings.Repeat("v", i+1)}})
	}
	m := Render(ms, "", testOpts(0, false, Layout{Width: 80, Height: 8, MaxRows: 8}))
	if rows := len(m.Lines) - 1; rows < 1 || rows > 3 { // minus the status line
		t.Errorf("8-line terminal: %d item rows, want 1-3", rows)
	}
	for _, lay := range []Layout{{Width: 80, Height: 4, MaxRows: 8}, {Width: 19, Height: 24, MaxRows: 8}} {
		if m := Render(ms, "", testOpts(0, false, lay)); len(m.Lines) != 0 {
			t.Errorf("%dx%d terminal: got %d lines, want no menu", lay.Width, lay.Height, len(m.Lines))
		}
	}
}

// NO_COLOR leaves attributes only (the > marker and reverse video); an
// empty NO_COLOR is unset, per no-color.org.
func TestNoColor(t *testing.T) {
	ms := []Match{{Item: Item{Value: "checkout", Description: "Switch branches"}, Positions: []int{0, 1}}, {Item: Item{Value: "cherry"}}}
	render := func(env map[string]string, theme string, colors int) string {
		th, _ := ResolveTheme(theme, "auto", colors, func(k string) string { return env[k] })
		opts := testOpts(0, true, Layout{Width: 60, Height: 24, MaxRows: 8})
		opts.Theme = th
		return strings.Join(Render(ms, "", opts).ANSI(), "\n")
	}
	colorSGR := regexp.MustCompile(`\x1b\[(?:[0-9]+;)*(?:3[0-9]|4[0-7]|9[0-7]|10[0-7])[;m]`)
	out := render(map[string]string{"NO_COLOR": "1"}, "dark", 256)
	if colorSGR.MatchString(out) {
		t.Errorf("NO_COLOR output has colors: %q", out)
	}
	if !strings.HasPrefix(out, "\x1b[1;7m>") {
		t.Errorf("NO_COLOR: the selected row must start with > in reverse video: %q", out)
	}
	if out := render(map[string]string{"NO_COLOR": ""}, "dark", 256); !colorSGR.MatchString(out) {
		t.Errorf("empty NO_COLOR must not disable colors: %q", out)
	}
	if out := render(nil, "dark", 8); strings.Contains(out, "38;5") || strings.Contains(out, "48;5") {
		t.Errorf("8-color terminal got 256-color escapes: %q", out)
	}
}

// Outside a UTF-8 locale every row is plain ASCII.
func TestASCIIGlyphs(t *testing.T) {
	gl := ResolveGlyphs("auto", 1, func(k string) string { return map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}[k] })
	switch {
	case runtime.GOOS == "windows":
		// No locale variables there: consoles are UTF-8.
		if gl != GlyphsUnicode {
			t.Fatalf("Windows ignores LC_ALL: got %+v", gl)
		}
		gl = ResolveGlyphs("on", 1, os.Getenv)
	case gl != GlyphsASCII:
		t.Fatalf("LC_ALL=C: got %+v", gl)
	}
	ms := []Match{{Item: Item{Value: strings.Repeat("y", 100), Description: strings.Repeat("d", 100)}, Positions: []int{0, 99}}}
	opts := testOpts(0, true, Layout{Width: 40, Height: 24, MaxRows: 8})
	opts.Glyphs = gl
	row := lineText(Render(ms, "", opts).Lines[0])
	for i := range len(row) {
		if row[i] >= 0x80 {
			t.Fatalf("non-ASCII byte in %q", row)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "...") {
		t.Errorf("truncated row must end with ...: %q", row)
	}
}

func lineText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// The zsh and bash integrations eval the engine's output; the quoting must
// survive a real shell.
func TestQuotePosixRoundTrip(t *testing.T) {
	input := "it's a \"test\"\n\ttab \\ back $HOME `cmd` \x1b[1m 한글"
	for _, sh := range []string{"bash", "zsh"} {
		path, err := exec.LookPath(sh)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, "-c", "printf %s "+QuotePosix(input)).Output()
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if string(out) != input {
			t.Errorf("%s round trip: got %q, want %q", sh, out, input)
		}
	}
}

func TestZshHighlightOffsetsCountCharacters(t *testing.T) {
	m := Menu{Lines: [][]Span{{{Text: "한글", Style: ThemeDark.Match}, {Text: "ab", Style: noStyle}}}}
	text, specs := m.ZshHighlights()
	if text != "\n한글ab" {
		t.Fatalf("text %q", text)
	}
	// Offsets are characters after the leading newline, not bytes.
	if len(specs) != 1 || !strings.HasPrefix(specs[0], "1 3 ") {
		t.Errorf("specs %q", specs)
	}
}

func TestFileCandidates(t *testing.T) {
	dir := t.TempDir()
	must(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644))

	c := fileCandidates(dir + "/")
	var got []string
	for _, it := range c.Items {
		got = append(got, it.Display)
	}
	if strings.Join(got, ",") != "b.txt,sub/,.hidden" {
		t.Errorf("listing %v; want directories marked and hidden entries last", got)
	}
	if c.Items[1].Value != dir+"/sub/" || !c.NoSpace(c.Items[1].Value) || c.NoSpace(c.Items[0].Value) {
		t.Errorf("directory values keep the prefix and suppress the trailing space: %+v", c.Items)
	}
}

func TestParseExternal(t *testing.T) {
	c := ParseExternal("-Path\tPath\t[string[]] Path\r\n./src/\tsrc\t\n\n")
	if !c.Verbatim || len(c.Items) != 2 {
		t.Fatalf("got %+v", c)
	}
	if it := c.Items[0]; it.Value != "-Path" || it.Label() != "Path" || it.Description != "[string[]] Path" {
		t.Errorf("item 0: %+v", it)
	}
	if !c.NoSpace(c.Items[1].Value) {
		t.Error("a directory value must not get a trailing space")
	}
}

// A fake carapace that logs each call proves the cache and the file fallback
// for commands no completer knows.
func TestFetchCachesAndFallsBackToFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake backend is a shell script")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	backend := filepath.Join(dir, "carapace")
	must(t, os.WriteFile(backend, []byte(`#!/bin/sh
echo "$*" >> `+calls+`
case "$1" in
  git) echo '{"nospace":"","usage":"","values":[{"value":"checkout","description":"Switch branches"}]}' ;;
esac
`), 0o755))
	work := t.TempDir()
	must(t, os.WriteFile(filepath.Join(work, "notes.md"), nil, 0o644))
	t.Chdir(work)

	cfg := Config{Carapace: backend, CacheDir: filepath.Join(dir, "cache"), TTL: time.Minute, Timeout: 5 * time.Second}
	for range 2 {
		c, err := Fetch(cfg, Parse(Zsh, "git ch"))
		if err != nil || len(c.Items) != 1 || c.Items[0].Value != "checkout" {
			t.Fatalf("git: %+v %v", c, err)
		}
	}
	if c, _ := Fetch(cfg, Parse(Zsh, "git chec")); len(c.Items) != 1 {
		t.Fatal("typing within the word must reuse the fetched candidates")
	}
	log, _ := os.ReadFile(calls)
	if n := strings.Count(string(log), "\n"); n != 1 {
		t.Errorf("backend ran %d times for one word, want 1:\n%s", n, log)
	}
	if !strings.HasPrefix(string(log), "git export git ") {
		t.Errorf("backend invocation %q", log)
	}

	c, err := Fetch(cfg, Parse(Zsh, "mytool no"))
	if err != nil || len(c.Items) != 1 || c.Items[0].Value != "notes.md" {
		t.Errorf("unknown command must fall back to files, got %+v %v", c, err)
	}
}

// Accepted words must name the same file when the shell evaluates them:
// a quoted ~ is not expanded, a newline inside double quotes after a
// backslash is a line continuation, and invalid UTF-8 must survive.
func TestAcceptedWordsEvaluateToTheFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file names and shells")
	}
	home := t.TempDir()
	work := t.TempDir()
	names := map[string]string{ // typed word -> file name
		"~/my":  "my dir",
		"nl":    "nl\nx",
		"caf":   "caf\xe9",
		"tab":   "tab\tz",
		"quote": "quote'd",
	}
	for typed, name := range names {
		dir := work
		if name == "my dir" {
			dir = home
		}
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			// APFS, for one, rejects file names that are not UTF-8.
			t.Logf("skipping %q: %v", name, err)
			delete(names, typed)
		}
	}
	for _, sh := range []Shell{Bash, Zsh} {
		path, err := exec.LookPath(string(sh))
		if err != nil {
			continue
		}
		for typed, name := range names {
			value := name
			if typed == "~/my" {
				value = "~/" + name
			}
			line := Parse(sh, "ls -d "+typed).Accept(&Candidates{}, value)
			cmd := exec.Command(path, "-c", line)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "HOME="+home)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s: %q does not name %q: %v %s", sh, line, name, err, out)
			}
		}
	}
}

// Until Up/Down chooses an item nothing is highlighted, so the menu never
// suggests that Enter would replace what was typed.
func TestRenderUnselected(t *testing.T) {
	var ms []Match
	for _, v := range []string{"a", "b", "c", "d", "e"} {
		ms = append(ms, Match{Item: Item{Value: v}})
	}
	m := Render(ms, "", testOpts(0, false, Layout{Width: 40, Height: 24, MaxRows: 3}))
	for _, line := range m.Lines {
		for _, sp := range line {
			if sp.Style.Reverse || sp.Text == ">" {
				t.Fatalf("unselected menu highlights %q", lineText(line))
			}
		}
	}
	if first := lineText(m.Lines[0]); !strings.HasPrefix(first, " a") {
		t.Errorf("unselected menu starts at the top, first row %q", first)
	}
	if status := strings.TrimSpace(lineText(m.Lines[len(m.Lines)-1])); status != "-/5" {
		t.Errorf("status line %q", status)
	}
}

// Same-case prefix matches rank first: -l before -L for "l".
func TestFilterPrefersTypedCase(t *testing.T) {
	ms := Filter([]Item{{Value: "-L"}, {Value: "-l"}, {Value: "-I"}, {Value: "-include"}}, "-", "l", nil)
	if ms[0].Value != "-l" {
		t.Errorf("want -l first, got %v", values(ms))
	}
	ms = Filter([]Item{{Value: "-I"}, {Value: "-include"}}, "-", "i", nil)
	if ms[0].Value != "-include" {
		t.Errorf("want -include first, got %v", values(ms))
	}
}

// The bash menu is drawn below the row the line ends on.
func TestEndRow(t *testing.T) {
	tests := []struct {
		prompt, line string
		cols, want   int
	}{
		{"$ ", "ls", 80, 0},
		{"$ ", strings.Repeat("x", 78), 80, 1}, // fills the row: cursor wraps
		{"$ ", strings.Repeat("x", 77), 80, 0},
		{"가나다 $ ", strings.Repeat("x", 72), 80, 1},   // Hangul: 2 columns each
		{"$ ", "a\nb", 80, 1},                        // pasted newline
		{"$ ", strings.Repeat("x", 37) + "한", 40, 1}, // wide char does not fit: wraps whole
	}
	for _, tt := range tests {
		if got := EndRow(tt.prompt, tt.line, tt.cols); got != tt.want {
			t.Errorf("EndRow(%q, %d chars, %d) = %d, want %d", tt.prompt, len([]rune(tt.line)), tt.cols, got, tt.want)
		}
	}
}

func TestPrintableStripsTerminalControls(t *testing.T) {
	in := "a\x1b[2Jb\u009b2Jc\nd\x7fe"
	got := Printable(in)
	if strings.ContainsAny(got, "\x1b\u009b\n\x7f") {
		t.Errorf("control characters left in %q", got)
	}
	if len([]rune(got)) != len([]rune(in)) {
		t.Errorf("Printable must keep rune positions: %q -> %q", in, got)
	}
}

func TestCacheIsPruned(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	keep := filepath.Join(dir, "unrelated.txt")
	must(t, os.WriteFile(old, []byte("{}"), 0o644))
	must(t, os.WriteFile(keep, nil, 0o644))
	stale := time.Now().Add(-2 * cacheMaxAge)
	must(t, os.Chtimes(old, stale, stale))
	must(t, os.Chtimes(keep, stale, stale))

	cfg := Config{CacheDir: dir}
	if _, err := cached(cfg, time.Minute, []string{"k"}, func() (*Candidates, error) { return &Candidates{}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("expired cache entry was not pruned")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("pruning must only touch ficus's own files")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// With a budget a slow completer never blocks the shell: a miss reports
// ErrLoading while the cache fills in the background, and an expired entry
// is served as it is while it refreshes.
func TestFetchBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake backend is a shell script")
	}
	dir := t.TempDir()
	backend := filepath.Join(dir, "carapace")
	must(t, os.WriteFile(backend, []byte(`#!/bin/sh
sleep 0.3
echo '{"nospace":"","usage":"","values":[{"value":"checkout"}]}'
`), 0o755))
	cfg := Config{Carapace: backend, CacheDir: filepath.Join(dir, "cache"), TTL: time.Minute, Timeout: 5 * time.Second, Budget: 50 * time.Millisecond}
	ctx := Parse(Zsh, "git ch")
	cfg.Spawn = func() error {
		go Fill(cfg, ctx)
		return nil
	}
	start := time.Now()
	if _, err := Fetch(cfg, ctx); err != ErrLoading {
		t.Fatalf("first fetch: err %v, want ErrLoading", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("a miss blocked for %v with a 50ms budget", d)
	}
	var c *Candidates
	var err error
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if c, err = Fetch(cfg, ctx); err != ErrLoading {
			break
		}
	}
	if err != nil || len(c.Items) != 1 {
		t.Fatalf("after the fill: %+v %v", c, err)
	}

	// An expired entry is served at once, with or without a budget (zsh
	// passes none: it is asynchronous already).
	key, _, _, _ := cacheKey(cfg, ctx)
	for _, budget := range []time.Duration{50 * time.Millisecond, 0} {
		cfg := cfg
		cfg.Budget = budget
		old := time.Now().Add(-time.Hour)
		must(t, os.Chtimes(cachePath(cfg, key), old, old))
		start = time.Now()
		if c, err := Fetch(cfg, ctx); err != nil || len(c.Items) != 1 || time.Since(start) > 100*time.Millisecond {
			t.Errorf("budget %v: expired entry must be served at once: %+v %v after %v", budget, c, err, time.Since(start))
		}
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if _, err := os.Stat(cachePath(cfg, key) + ".lock"); os.IsNotExist(err) {
				break
			}
		}
	}
	// Let the refresh finish before TempDir cleanup.
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(cachePath(cfg, key) + ".lock"); os.IsNotExist(err) {
			break
		}
	}
}

// Without a filter, tagged items are listed under one header per tag, and
// group movement jumps between the groups' first items.
func TestGroups(t *testing.T) {
	ms := []Match{
		{Item: Item{Value: "add", Tag: "A"}},
		{Item: Item{Value: "zap", Tag: "B"}},
		{Item: Item{Value: "am", Tag: "A"}},
	}
	ordered := GroupOrder(ms, true, "")
	if got := strings.Join(values(ordered), ","); got != "add,am,zap" {
		t.Fatalf("group order %s", got)
	}
	opts := testOpts(0, false, Layout{Width: 60, Height: 24, MaxRows: 8})
	opts.Groups = true
	var headers []string
	for _, l := range Render(ordered, "", opts).Lines {
		if text := lineText(l); strings.HasPrefix(text, " ──") {
			headers = append(headers, strings.TrimSpace(strings.TrimPrefix(text, " ──")))
		}
	}
	if strings.Join(headers, ",") != "A,B" {
		t.Errorf("headers %q, want A,B", headers)
	}
	opts.Pattern = "a"
	if m := Render(ordered, "", opts); strings.Contains(lineText(m.Lines[0]), "──") {
		t.Error("a typed filter ranks items: no groups")
	}

	if got := MoveGroup(ordered, true, 0, true, 1, 8); got != 2 {
		t.Errorf("next group from 0: %d, want 2", got)
	}
	if got := MoveGroup(ordered, true, 2, true, -1, 8); got != 0 {
		t.Errorf("previous group from 2: %d, want 0", got)
	}
	if got := MoveGroup(ordered, true, 1, true, -1, 8); got != 0 {
		t.Errorf("previous group from inside group A goes to its first item: %d", got)
	}
}

// A description too long for its column is wrapped below the rows.
func TestDetailsPane(t *testing.T) {
	desc := strings.Repeat("lorem ipsum ", 25) // 300 characters
	ms := []Match{{Item: Item{Value: "item", Description: desc}}, {Item: Item{Value: "other"}}}
	opts := testOpts(0, true, Layout{Width: 60, Height: 24, MaxRows: 8, Details: 3})
	m := Render(ms, "", opts)
	var details []string
	for _, l := range m.Lines[2:] { // after the two item rows
		if text := lineText(l); strings.HasPrefix(text, "   ") {
			details = append(details, text)
		}
	}
	if len(details) == 0 || len(details) > 3 {
		t.Fatalf("got %d detail lines, want 1-3: %q", len(details), details)
	}
	for _, d := range details {
		if w := runewidth.StringWidth(d); w >= 60 {
			t.Errorf("detail line is %d columns wide: %q", w, d)
		}
	}
	if !strings.HasSuffix(details[len(details)-1], "…") {
		t.Errorf("cut text ends in an ellipsis: %q", details[len(details)-1])
	}
}

// An accepted value ranks above equally good matches next time.
func TestRecentRanksFirst(t *testing.T) {
	cfg := Config{CacheDir: t.TempDir()}
	ctx := Parse(Zsh, "git re")
	items := []Item{{Value: "reset"}, {Value: "rebase"}}
	if got := values(Filter(items, "", "re", LoadRecent(cfg, ctx))); got[0] != "reset" {
		t.Fatalf("without history: %v", got)
	}
	must(t, RecordRecent(cfg, ctx, "rebase"))
	if got := values(Filter(items, "", "re", LoadRecent(cfg, ctx))); got[0] != "rebase" {
		t.Errorf("recently accepted rebase must rank first: %v", got)
	}
	if got := LoadRecent(cfg, Parse(Zsh, "ls re")); len(got) != 0 {
		t.Errorf("history is per command: %v", got)
	}
}

// Flags of a command carapace does not know come from its --help: GNU and
// oclif layouts, without their value placeholders.
func TestParseHelp(t *testing.T) {
	help := "Usage: tool [options]\n\nFLAGS\n" +
		"  -p, --print                 Non-interactive mode\n" +
		"      --model=<value>         Model to use\n" +
		"  -o FILE, --output[=FILE]    Write to FILE\n" +
		"\x1b[1m  --color\x1b[0m  Colorize\n" +
		"--not-indented  ignored\n" +
		"  omp agents unpack --project - Export bundled subagents\n"
	var got []string
	for _, it := range parseHelp(help) {
		got = append(got, it.Value+"|"+it.Description)
	}
	want := []string{
		"-p|Non-interactive mode", "--print|Non-interactive mode",
		"--model|Model to use",
		"-o|Write to FILE", "--output|Write to FILE",
		"--color|Colorize",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("parseHelp:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// --help is never run for a script: it may ignore the flag and do its work.
// On Windows the script is a batch file, which PATH lookup finds as well.
func TestHelpSkipsScripts(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	if runtime.GOOS == "windows" {
		must(t, os.WriteFile(filepath.Join(dir, "deploy.cmd"), []byte("@echo off\r\ntype nul > \""+ran+"\"\r\necho   --force  Do it\r\n"), 0o755))
	} else {
		must(t, os.WriteFile(filepath.Join(dir, "deploy"), []byte("#!/bin/sh\ntouch "+ran+"\necho '  --force  Do it'\n"), 0o755))
	}
	t.Setenv("PATH", dir)
	c, err := helpCandidates(Config{CacheDir: filepath.Join(dir, "cache"), Timeout: 5 * time.Second}, "deploy")
	if err != nil || len(c.Items) != 0 {
		t.Errorf("got %+v %v", c, err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("a script was run with --help")
	}
}

// Enter runs the line as typed when it is already complete — the bug
// reports these come from: `cd ../` completed into a subdirectory, `ls ` into
// the first file, `git add .` into a fuzzy match, `git log` needed two Enters.
func TestRunAsTyped(t *testing.T) {
	files := []Item{{Value: "foo.tar.gz", Kind: ItemFile}, {Value: "notes.txt", Kind: ItemFile}, {Value: "notes.txt.bak", Kind: ItemFile}, {Value: "src/", Kind: ItemDir}}
	parent := []Item{{Value: "../alpha/", Kind: ItemDir}, {Value: "../beta/", Kind: ItemDir}}
	subs := []Item{{Value: "log"}, {Value: "reflog"}, {Value: "shortlog"}, {Value: "checkout"}, {Value: "cherry"}}
	flags := []Item{{Value: "-b", Kind: ItemFlag}, {Value: "-n", Kind: ItemFlag}}
	tests := []struct {
		line  string
		items []Item
		run   bool
	}{
		{"cd ../", parent, true},       // a directory prefix with nothing after it
		{"ls ", files, true},           // an empty word after a space
		{"cat -", flags, true},         // a lone dash (stdin, previous branch)
		{"git add .", files, true},     // "." names a directory
		{"cd ..", files, true},         // ".." too; foo.tar.gz only matches fuzzily
		{"git log", subs, true},        // names a candidate: completing adds only a space
		{"cat notes.txt", files, true}, // also when a longer candidate follows
		{"cd src", files, true},        // a directory without its trailing slash
		{"grep oo", files, true},       // no candidate starts with the word
		{"git ch", subs, false},        // the start of longer candidates: Enter completes
		{"rm notes", files, false},
		{"git lo", subs, false},
	}
	for _, tt := range tests {
		c := Parse(Zsh, tt.line)
		if got := RunAsTyped(Filter(tt.items, c.FetchPrefix, c.Pattern, nil), c); got != tt.run {
			t.Errorf("RunAsTyped(%q) = %v, want %v", tt.line, got, tt.run)
		}
	}
	if RunAsTyped(nil, Parse(Zsh, "ls ")) {
		t.Error("no candidates, no menu: nothing to run from")
	}
}

// The run row is highlighted while no item is selected; once Down selects
// an item it stays on top, unmarked.
func TestRenderRunRow(t *testing.T) {
	ms := []Match{{Item: Item{Value: "log"}}, {Item: Item{Value: "reflog"}}}
	opts := testOpts(0, false, Layout{Width: 40, Height: 24, MaxRows: 8})
	opts.RunRow = true
	lines := Render(ms, "", opts).Lines
	if got := lineText(lines[0]); !strings.HasPrefix(got, ">↪ run") {
		t.Errorf("run row %q, want it marked and first", got)
	}
	if got := lineText(lines[1]); !strings.HasPrefix(got, " log") {
		t.Errorf("first item %q must not be marked", got)
	}
	opts.Selected = true
	lines = Render(ms, "", opts).Lines
	if !strings.HasPrefix(lineText(lines[0]), " ↪ run") || !strings.HasPrefix(lineText(lines[1]), ">log") {
		t.Errorf("after Down: %q / %q", lineText(lines[0]), lineText(lines[1]))
	}
}
