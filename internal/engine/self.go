package engine

import (
	"path/filepath"
	"strings"
)

// SelfCommand is a ficus subcommand as its own completion offers it.
// carapace has no completer for ficus, and ficus's usage text is not a flag
// list parseHelp reads, so ficus completes itself from this table. It
// follows cmd/ficus; a test there checks the flags against the real flag
// sets. complete, accept, pick and fill are called by the shell
// integrations, not typed, and are not listed.
type SelfCommand struct {
	Name, Description string
	Flags             []SelfFlag
	Args              []string // values of the first positional argument
}

// SelfFlag is a flag of a SelfCommand; Values, when set, are what the flag
// takes (a comma-separated list of them).
type SelfFlag struct {
	Name, Description string
	Values            []string
}

// SelfCommands lists ficus's subcommands in the order the menu shows them.
var SelfCommands = []SelfCommand{
	{Name: "setup", Description: "load ficus in every new shell (startup file blocks)", Flags: []SelfFlag{
		{Name: "shells", Description: "shells to set up, comma-separated", Values: []string{"zsh", "bash", "pwsh", "powershell"}},
		{Name: "remove", Description: "delete the block from every startup file"},
	}},
	{Name: "update", Description: "install the latest release over this ficus", Flags: []SelfFlag{
		{Name: "check", Description: "only report whether a newer release exists"},
		{Name: "force", Description: "install the latest release even over a source build or the same version"},
	}},
	{Name: "doctor", Description: "show what ficus detects about the terminal and settings"},
	{Name: "init", Description: "print the shell integration", Args: []string{"zsh", "bash", "pwsh"}},
	{Name: "version", Description: "print the version"},
	{Name: "help", Description: "show the usage"},
}

// isSelf reports whether the command word runs ficus (by name or path).
func isSelf(word string) bool {
	name := filepath.Base(strings.ReplaceAll(word, `\`, "/"))
	return strings.TrimSuffix(strings.ToLower(name), ".exe") == "ficus"
}

// selfCandidates completes c, a ficus command line: subcommands, then each
// subcommand's flags, flag values (--shells zsh, --shells=zsh,bash) and
// positional values (init zsh).
func selfCandidates(c *Context) *Candidates {
	words := c.Words
	if len(words) == 2 {
		var items []Item
		for _, sc := range SelfCommands {
			items = append(items, Item{Value: sc.Name, Description: sc.Description, Kind: ItemCmd})
		}
		return &Candidates{Items: items}
	}
	var sc *SelfCommand
	for i := range SelfCommands {
		if SelfCommands[i].Name == words[1] {
			sc = &SelfCommands[i]
		}
	}
	if sc == nil {
		return &Candidates{}
	}
	flagNamed := func(word string) *SelfFlag {
		name := strings.TrimLeft(word, "-")
		if name == word || name == "" {
			return nil
		}
		for i := range sc.Flags {
			if sc.Flags[i].Name == name {
				return &sc.Flags[i]
			}
		}
		return nil
	}
	values := func(f *SelfFlag) *Candidates {
		out := &Candidates{}
		for _, v := range f.Values {
			out.Items = append(out.Items, Item{Value: c.FetchPrefix + v, Display: v})
		}
		return out
	}
	// --shells=zsh,b: the value of the flag before "=".
	if name, _, ok := strings.Cut(c.Current, "="); ok {
		if f := flagNamed(name); f != nil {
			return values(f)
		}
		return &Candidates{}
	}
	// --shells zsh,b: the value of the previous word's flag.
	if f := flagNamed(words[len(words)-2]); f != nil && f.Values != nil {
		return values(f)
	}
	out := &Candidates{}
	if len(words) == 3 && !strings.HasPrefix(c.Current, "-") {
		for _, a := range sc.Args {
			out.Items = append(out.Items, Item{Value: a})
		}
	}
	for _, f := range sc.Flags {
		out.Items = append(out.Items, Item{Value: "--" + f.Name, Description: f.Description, Kind: ItemFlag})
	}
	return out
}
