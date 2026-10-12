package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
)

// Item is one completion candidate.
type Item struct {
	Value       string   `json:"value"`
	Display     string   `json:"display,omitempty"`
	Description string   `json:"description,omitempty"`
	Tag         string   `json:"tag,omitempty"`
	Kind        ItemKind `json:"kind,omitempty"`
}

// ItemKind says what an item is, for the menu's icon column.
type ItemKind string

const (
	ItemCmd     ItemKind = "cmd"
	ItemAlias   ItemKind = "alias"
	ItemFunc    ItemKind = "func"
	ItemBuiltin ItemKind = "builtin"
	ItemFlag    ItemKind = "flag"
	ItemDir     ItemKind = "dir"
	ItemFile    ItemKind = "file"
	ItemVar     ItemKind = "var"
)

// valueKind classifies a completion value by its shape: a flag, or a
// directory (trailing separator).
func valueKind(v, dirSeps string) ItemKind {
	switch {
	case strings.HasPrefix(v, "-"):
		return ItemFlag
	case v != "" && strings.ContainsRune(dirSeps, rune(v[len(v)-1])):
		return ItemDir
	}
	return ""
}

// Label is the text shown in the menu.
func (i Item) Label() string {
	if i.Display != "" {
		return i.Display
	}
	return i.Value
}

// Candidates is the cached result of one fetch.
type Candidates struct {
	Items []Item `json:"values"`
	// Nospace lists value suffixes after which no space is appended; "*"
	// means never append one.
	Nospace string `json:"nospace"`
	// Usage is shown when nothing matches (e.g. a free-form flag value).
	Usage string `json:"usage"`
	// Verbatim values are already quoted for the shell (they come from the
	// shell's own completer) and are inserted as they are.
	Verbatim bool `json:"-"`
}

// NoSpace reports whether accepting v should keep the cursor in the word.
func (c *Candidates) NoSpace(v string) bool {
	if c.Nospace == "*" {
		return true
	}
	return v != "" && strings.ContainsRune(c.Nospace, []rune(v)[len([]rune(v))-1])
}

// Config locates the backend and the cache.
type Config struct {
	Carapace string        // path of ficus-carapace
	CacheDir string        // directory for cached candidate lists
	TTL      time.Duration // lifetime of argument candidates
	Timeout  time.Duration // limit for one completer run
	// ExtraCommands is an optional file with "name\tdescription" lines the
	// shell integration writes at startup (aliases, functions, cmdlets).
	ExtraCommands string
	// Spawn, when set, starts a background fill (ficus fill): an expired
	// cache entry (up to a day old) is returned at once while it refreshes.
	// Budget, when positive, also bounds how long Fetch waits for a cache
	// miss before returning ErrLoading; zero fills a miss synchronously.
	Budget time.Duration
	Spawn  func() error
	// HelpFlags reads flags from `cmd --help` for commands carapace does
	// not know.
	HelpFlags bool
}

// ErrLoading reports that candidates are being fetched in the background
// and did not arrive within Config.Budget.
var ErrLoading = errors.New("loading")

// cacheKey returns the cache key and lifetime of c's backend candidates;
// ok is false for sources that are read directly (files, variables).
func cacheKey(cfg Config, c *Context) (key []string, ttl time.Duration, extra []byte, ok bool) {
	switch c.Kind {
	case KindVariable, KindFile:
		return nil, 0, nil, false
	case KindCommand:
		// Keyed on the list's content, not its path: the path is per shell,
		// the content is shared by every shell with the same aliases.
		extra, _ = os.ReadFile(cfg.ExtraCommands)
		return []string{"commands", os.Getenv("PATH"), string(extra)}, time.Minute, extra, true
	}
	wd, _ := os.Getwd()
	key = append([]string{"args", string(c.Shell), wd, c.FetchPrefix}, c.Words[:len(c.Words)-1]...)
	return key, cfg.TTL, nil, true
}

// Fetch returns the unfiltered candidates for c.
func Fetch(cfg Config, c *Context) (*Candidates, error) {
	key, ttl, extra, ok := cacheKey(cfg, c)
	if !ok {
		if c.Kind == KindVariable {
			return envCandidates(), nil
		}
		return fileCandidates(c.FetchPrefix), nil
	}
	if c.Kind == KindCommand {
		return cached(cfg, ttl, key, func() (*Candidates, error) {
			return commandCandidates(cfg, extra)
		})
	}
	cands, err := cached(cfg, ttl, key, func() (*Candidates, error) {
		return carapaceCandidates(cfg, c)
	})
	if err != nil {
		return nil, err
	}
	if len(cands.Items) == 0 && cands.Usage == "" {
		// carapace has no completer for this command. A dashed word asks
		// for flags: read them from the command's --help. Otherwise offer
		// files, like every shell does by default.
		if cfg.HelpFlags && strings.HasPrefix(c.Current, "-") {
			help, err := helpCandidates(cfg, c.Words[0])
			if err != nil {
				return nil, err
			}
			if help = withPrefix(help, c.FetchPrefix); len(help.Items) > 0 {
				return help, nil
			}
		}
		return fileCandidates(c.FetchPrefix), nil
	}
	return cands, nil
}

// Fill fetches c's candidates into the cache synchronously, for the
// background process Config.Spawn starts, and releases the fill lock.
func Fill(cfg Config, c *Context) error {
	cfg.Budget = 0
	if key, ttl, _, ok := cacheKey(cfg, c); ok && cfg.CacheDir != "" && ttl > 0 {
		defer os.Remove(cachePath(cfg, key) + ".lock")
	}
	_, err := Fetch(cfg, c)
	return err
}

// ParseExternal reads candidates supplied by the shell itself, one per line
// as "value<TAB>label<TAB>description" (label and description optional).
func ParseExternal(tsv string) *Candidates {
	c := &Candidates{Nospace: "/\\", Verbatim: true}
	for line := range strings.SplitSeq(tsv, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 3)
		it := Item{Value: f[0]}
		if len(f) > 1 && f[1] != f[0] {
			it.Display = f[1]
		}
		if len(f) > 2 {
			it.Description = f[2]
		}
		it.Kind = valueKind(it.Value, `/\`)
		c.Items = append(c.Items, it)
	}
	return c
}

func carapaceCandidates(cfg Config, c *Context) (*Candidates, error) {
	args := []string{c.Words[0], "export", c.Words[0]}
	args = append(args, c.Words[1:len(c.Words)-1]...)
	args = append(args, c.FetchPrefix)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.Carapace, args...)
	cmd.Stdin = nil
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("completer timed out")
		}
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return nil, err // backend missing or not executable
		}
	}
	cands := &Candidates{}
	if len(bytes.TrimSpace(out)) == 0 {
		return cands, nil // unknown command
	}
	if err := json.Unmarshal(out, cands); err != nil {
		return nil, err
	}
	for i := range cands.Items {
		it := &cands.Items[i]
		it.Kind = valueKind(it.Value, "")
		if it.Kind == "" && (strings.Contains(it.Tag, "file") || strings.Contains(it.Tag, "director")) {
			it.Kind = ItemFile
			if strings.HasSuffix(it.Value, "/") {
				it.Kind = ItemDir
			}
		}
	}
	return cands, nil
}

// cachePath is where the candidates for key are cached.
func cachePath(cfg Config, key []string) string {
	sum := sha256.Sum256([]byte(strings.Join(key, "\x00")))
	return filepath.Join(cfg.CacheDir, hex.EncodeToString(sum[:12])+".json")
}

func readCache(path string) (*Candidates, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var c Candidates
	if json.Unmarshal(data, &c) != nil {
		return nil, false
	}
	return &c, true
}

// cached runs fill unless a fresh result for key exists on disk. When the
// caller can start background fills (Config.Spawn), an expired result is
// returned as it is while a fill refreshes it, and with a budget a miss is
// filled in the background too (see Config.Budget).
func cached(cfg Config, ttl time.Duration, key []string, fill func() (*Candidates, error)) (*Candidates, error) {
	if cfg.CacheDir == "" || ttl <= 0 {
		return fill()
	}
	path := cachePath(cfg, key)
	revalidate := cfg.Spawn != nil
	async := cfg.Budget > 0 && revalidate

	if st, err := os.Stat(path); err == nil {
		age := time.Since(st.ModTime())
		fresh := age < ttl
		if fresh || (revalidate && age < cacheMaxAge) {
			if c, ok := readCache(path); ok {
				if !fresh {
					startFill(cfg, path) // stale while revalidating
				}
				return c, nil
			}
		}
	}
	if async {
		startFill(cfg, path)
		deadline := time.Now().Add(cfg.Budget)
		for time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
			if _, err := os.Stat(path); err == nil {
				if c, ok := readCache(path); ok {
					return c, nil
				}
			}
		}
		return nil, ErrLoading
	}

	c, err := fill()
	if err != nil {
		return nil, err
	}
	if data, err := json.Marshal(c); err == nil {
		if writeAtomic(cfg.CacheDir, path, data) == nil {
			// A background fill's lock for this entry is obsolete now.
			os.Remove(path + ".lock")
			prune(cfg.CacheDir)
		}
	}
	return c, nil
}

// writeAtomic writes data to path in dir by write-then-rename, so a
// concurrent reader never sees a partial file.
func writeAtomic(dir, path string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// startFill spawns a background fill for path unless one is running: the
// lock file is created exclusively, and one older than a completer run can
// take belongs to a fill that died.
func startFill(cfg Config, path string) {
	lock := path + ".lock"
	if os.MkdirAll(cfg.CacheDir, 0o700) != nil {
		return
	}
	for range 2 {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			if cfg.Spawn() != nil {
				os.Remove(lock)
			}
			return
		}
		st, serr := os.Stat(lock)
		if serr == nil && time.Since(st.ModTime()) < cfg.Timeout+2*time.Second {
			return // a fill is running
		}
		os.Remove(lock)
	}
}

// Cache entries older than this are deleted; nothing is cached longer.
const cacheMaxAge = 24 * time.Hour

// prune deletes expired entries, at most once an hour (tracked by the
// mtime of a marker file), so the cache does not grow without bound.
func prune(dir string) {
	marker := filepath.Join(dir, ".pruned")
	if st, err := os.Stat(marker); err == nil && time.Since(st.ModTime()) < time.Hour {
		return
	}
	if f, err := os.Create(marker); err == nil {
		f.Close()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		maxAge := cacheMaxAge
		switch {
		case strings.HasSuffix(name, ".lock"):
			maxAge = time.Hour // left by a fill that died
		case name == recentFile: // history, not cache
			continue
		case !strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".tmp-"):
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

func commandCandidates(cfg Config, extra []byte) (*Candidates, error) {
	descs := map[string]string{}
	seen := map[string]bool{}
	var items []Item
	add := func(name, desc string, kind ItemKind) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		items = append(items, Item{Value: name, Description: desc, Kind: kind})
	}

	for line := range strings.SplitSeq(string(extra), "\n") {
		name, desc, _ := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		var kind ItemKind
		switch {
		case desc == "alias" || strings.HasPrefix(desc, "alias:"):
			kind = ItemAlias
		case desc == "function" || desc == "cmdlet" || desc == "filter":
			kind = ItemFunc
		case desc == "builtin":
			kind = ItemBuiltin
		}
		add(name, desc, kind)
	}

	for name, desc := range completerDescriptions(cfg) {
		descs[name] = desc
	}
	for _, name := range pathExecutables() {
		add(name, descs[name], ItemCmd)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Value < items[j].Value })
	return &Candidates{Items: items}, nil
}

// completerDescriptions maps command names to carapace's one-line
// descriptions. carapace --list is slow, so it is cached for a day.
func completerDescriptions(cfg Config) map[string]string {
	c, err := cached(cfg, cacheMaxAge, []string{"completers", cfg.Carapace}, func() (*Candidates, error) {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, cfg.Carapace, "--list").Output()
		if err != nil {
			return nil, err
		}
		var list map[string][]struct {
			Description string `json:"description"`
		}
		if err := json.Unmarshal(out, &list); err != nil {
			return nil, err
		}
		c := &Candidates{}
		for name, variants := range list {
			if len(variants) > 0 {
				c.Items = append(c.Items, Item{Value: name, Description: variants[0].Description})
			}
		}
		return c, nil
	})
	m := map[string]string{}
	if err == nil {
		for _, it := range c.Items {
			m[it.Value] = it.Description
		}
	}
	return m
}

func pathExecutables() []string {
	var exts []string
	if runtime.GOOS == "windows" {
		exts = strings.Split(strings.ToLower(os.Getenv("PATHEXT")), ";")
		if len(exts) == 1 && exts[0] == "" {
			exts = []string{".com", ".exe", ".bat", ".cmd"}
		}
	}
	var names []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if runtime.GOOS == "windows" {
				ext := strings.ToLower(filepath.Ext(name))
				if ext == "" || !slices.Contains(exts, ext) {
					continue
				}
				names = append(names, strings.TrimSuffix(name, filepath.Ext(name)))
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				if info, err = os.Stat(filepath.Join(dir, name)); err != nil {
					continue
				}
			}
			if !info.IsDir() && info.Mode()&0o111 != 0 {
				names = append(names, name)
			}
		}
	}
	return names
}

// fileCandidates lists the directory named by prefix ("" = cwd). Values keep
// the typed prefix so they replace the whole word.
func fileCandidates(prefix string) *Candidates {
	dir := prefix
	if strings.HasPrefix(dir, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home + dir[1:]
		}
	}
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	c := &Candidates{Nospace: "/\\"}
	if err != nil {
		return c
	}
	sep := "/"
	if strings.HasSuffix(prefix, `\`) {
		sep = `\`
	}
	for _, e := range entries {
		name := e.Name()
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil {
				isDir = st.IsDir()
			}
		}
		kind := ItemFile
		if isDir {
			name += sep
			kind = ItemDir
		}
		c.Items = append(c.Items, Item{Value: prefix + name, Display: name, Kind: kind})
	}
	// Hidden entries last, the shell convention.
	sort.SliceStable(c.Items, func(i, j int) bool {
		hi, hj := strings.HasPrefix(c.Items[i].Display, "."), strings.HasPrefix(c.Items[j].Display, ".")
		if hi != hj {
			return hj
		}
		return c.Items[i].Display < c.Items[j].Display
	})
	return c
}

func envCandidates() *Candidates {
	c := &Candidates{Nospace: "*"}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "" {
			c.Items = append(c.Items, Item{Value: name, Kind: ItemVar})
		}
	}
	sort.Slice(c.Items, func(i, j int) bool { return c.Items[i].Value < c.Items[j].Value })
	return c
}
