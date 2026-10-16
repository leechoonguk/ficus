package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// helpCandidates lists the flags a command documents in its --help output,
// for commands carapace has no completer for (zsh's _gnu_generic does the
// same). Only programs found in PATH are asked, and only those helpRunnable
// allows: never scripts, which may ignore --help and do their work instead,
// nor Windows GUI programs. The result is cached for as long as the
// executable does not change.
func helpCandidates(cfg Config, name string) (*Candidates, error) {
	if strings.ContainsAny(name, `/\`) {
		return &Candidates{}, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return &Candidates{}, nil
	}
	st, err := os.Stat(path)
	if err != nil || !helpRunnable(path) {
		return &Candidates{}, nil
	}
	key := []string{"help", path, fmt.Sprint(st.ModTime().UnixNano(), st.Size())}
	return cached(cfg, cacheMaxAge, key, func() (*Candidates, error) {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, "--help")
		cmd.Stdin = nil // /dev/null: nothing waits for input
		cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb", "COLUMNS=200", "PAGER=cat")
		out, _ := cmd.CombinedOutput() // many print help to stderr, exit non-zero
		return &Candidates{Items: parseHelp(string(out))}, nil
	})
}

var (
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	flagName   = regexp.MustCompile(`^--?[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	// Two spaces or a tab end the flag spec and start its description.
	specEnd = regexp.MustCompile(`\s{2,}|\t`)
)

// parseHelp reads flag lines of --help output: an indented line starting
// with "-", flag spellings separated by commas or spaces, then two spaces
// and a description:
//
//	-p, --print                 Non-interactive mode
//	    --model=<value>         Model to use
//	-o FILE, --output[=FILE]    Write to FILE
//
// The value placeholder ("=<value>", " FILE") is not part of the item:
// accepting a flag inserts it followed by a space, which parsers that take
// "--flag=value" also accept as "--flag value".
func parseHelp(text string) []Item {
	var items []Item
	seen := map[string]bool{}
	for line := range strings.SplitSeq(ansiEscape.ReplaceAllString(text, ""), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimLeft(line, " ")
		if len(trimmed) == len(line) || !strings.HasPrefix(trimmed, "-") {
			continue // flag lines are indented
		}
		spec, desc := trimmed, ""
		if loc := specEnd.FindStringIndex(trimmed); loc != nil {
			spec, desc = trimmed[:loc[0]], strings.TrimSpace(trimmed[loc[1]:])
		}
		for tok := range strings.FieldsSeq(strings.ReplaceAll(spec, ",", " ")) {
			name := tok
			if i := strings.IndexAny(tok, "=[<"); i > 0 {
				name = tok[:i]
			}
			if !flagName.MatchString(name) || seen[name] {
				continue
			}
			seen[name] = true
			items = append(items, Item{Value: name, Description: desc, Kind: ItemFlag})
		}
	}
	return items
}

// withPrefix keeps the items that continue prefix (the typed dashes).
func withPrefix(c *Candidates, prefix string) *Candidates {
	out := *c
	out.Items = nil
	for _, it := range c.Items {
		if strings.HasPrefix(it.Value, prefix) {
			out.Items = append(out.Items, it)
		}
	}
	return &out
}
