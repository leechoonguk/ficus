// Package engine computes completion menus for a partially typed command
// line: it parses the line, gathers candidates (carapace, PATH, files,
// environment), ranks them with fzf's matcher and renders the menu.
package engine

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	shlex "github.com/carapace-sh/carapace-shlex/v2"
)

// Shell identifies the integration that called the engine. It selects the
// lexer, the quoting rules and the output syntax.
type Shell string

const (
	Zsh        Shell = "zsh"
	Bash       Shell = "bash"
	Powershell Shell = "pwsh"
)

// ParseShell validates a --shell value.
func ParseShell(s string) (Shell, error) {
	switch Shell(s) {
	case Zsh, Bash, Powershell:
		return Shell(s), nil
	case "powershell":
		return Powershell, nil
	}
	return "", fmt.Errorf("unsupported shell %q (want zsh, bash or pwsh)", s)
}

func (s Shell) format() shlex.Format {
	switch s {
	case Zsh:
		return shlex.Zsh
	case Powershell:
		return shlex.Powershell
	default:
		return shlex.Bash
	}
}

// Kind tells which candidate source applies to the word under the cursor.
type Kind int

const (
	KindCommand  Kind = iota // first word of a command: executables
	KindArgument             // argument of a command: carapace, files as fallback
	KindFile                 // redirect target or path-like command
	KindVariable             // $NAME reference
)

// Context is the parsed state of the line left of the cursor.
type Context struct {
	Shell Shell
	Kind  Kind

	// Words of the current command (pipeline segment or innermost $(...)),
	// dequoted. The last element is the word being completed.
	Words []string
	// Current is the dequoted word being completed.
	Current string
	// FetchPrefix is the head of Current that candidates are fetched for:
	// everything up to and including the last separator. Candidates for one
	// FetchPrefix are cached and the rest of the word filters them, so typing
	// within a word never re-runs a completer.
	FetchPrefix string
	// Pattern is the tail of Current after FetchPrefix: the fuzzy query.
	Pattern string

	head string // line left of the replaced word
	sh   *shlex.CompletionContext
}

// separators end a FetchPrefix. "/" covers paths, "=" flag values
// (--format=), "," list values, ":" qualified values (image:tag, remote:ref).
const separators = "/=,:"

// Parse builds the completion context for the text left of the cursor.
func Parse(shell Shell, lbuffer string) *Context {
	sh := shlex.Complete(lbuffer, shell.format())
	c := &Context{Shell: shell, sh: sh, Words: sh.Words, Current: sh.CurrentWord}
	if len(c.Words) == 0 {
		c.Words = []string{""}
	}

	// Span counts runes, one per invalid UTF-8 byte; slice by bytes so such
	// bytes (e.g. Latin-1 file names) survive in the rest of the line.
	c.head = lbuffer[:byteOffset(lbuffer, max(sh.Span.Start, 0))]

	pathSeps := "/"
	if shell == Powershell {
		pathSeps += `\`
	}
	seps := separators
	switch {
	case sh.Variable != nil:
		c.Kind, seps = KindVariable, ""
	case sh.IsRedirect:
		c.Kind, seps = KindFile, pathSeps
	case len(c.Words) <= 1 && strings.ContainsAny(c.Current, pathSeps):
		c.Kind, seps = KindFile, pathSeps // ./script, bin/tool
	case len(c.Words) <= 1:
		c.Kind, seps = KindCommand, ""
	default:
		c.Kind = KindArgument
		if shell == Powershell {
			seps += `\`
		}
	}

	if c.Kind == KindVariable {
		c.Pattern = sh.Variable.Name
		return c
	}
	cut := strings.LastIndexAny(c.Current, seps) + 1 // 0 when seps is empty
	if cut == 0 && c.Kind == KindArgument && strings.HasPrefix(c.Current, "-") {
		// Completers only offer flags for a word that starts with a dash.
		cut = 1
		if strings.HasPrefix(c.Current, "--") {
			cut = 2
		}
	}
	c.FetchPrefix, c.Pattern = c.Current[:cut], c.Current[cut:]
	return c
}

// Accept returns the new left buffer after choosing value from cands. The
// value replaces the current word, quoted for the shell unless it already
// is; a space follows unless the value continues (directory, flag=, ...).
func (c *Context) Accept(cands *Candidates, value string) string {
	word := value
	switch {
	case cands.Verbatim:
	case c.Kind == KindVariable:
		word = c.sh.Variable.Insert(value)
	default:
		word = c.quote(value)
	}
	if cands.NoSpace(value) {
		return c.head + word
	}
	return c.head + word + " "
}

// AcceptPartial returns the new left buffer with the current word replaced
// by value, the common start of several candidates: quoted like Accept, but
// with nothing after it, so typing continues the word.
func (c *Context) AcceptPartial(value string) string {
	if c.Kind == KindVariable {
		// Leave a ${ expansion open.
		return c.head + strings.TrimSuffix(c.sh.Variable.Insert(value), "}")
	}
	return c.head + c.quote(value)
}

func (c *Context) quote(value string) string {
	posix := c.Shell == Zsh || c.Shell == Bash
	if posix && needsANSIC(value) {
		// Double quotes would turn a newline into a line continuation and
		// cannot carry invalid UTF-8; $'...' carries any byte. It replaces the
		// whole raw word, including an opening quote the user typed.
		return QuotePosix(value)
	}
	open := c.sh.QuotingState != shlex.START_STATE && c.sh.QuotingState != shlex.IN_WORD_STATE
	if prefix, rest, ok := tildePrefix(value); ok && !open {
		// A quoted ~ is not expanded: keep ~/ or ~user/ bare.
		if rest == "" {
			return prefix
		}
		return prefix + c.sh.Quote(rest)
	}
	return c.sh.Quote(value)
}

// tildePrefix splits "~/x" or "~user/x" into "~/" or "~user/" and "x".
func tildePrefix(s string) (prefix, rest string, ok bool) {
	if !strings.HasPrefix(s, "~") {
		return "", "", false
	}
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return "", "", false
	}
	for _, r := range s[1:i] {
		if !(r == '.' || r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return "", "", false
		}
	}
	return s[:i+1], s[i+1:], true
}

// needsANSIC reports control characters or invalid UTF-8.
func needsANSIC(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	return strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// byteOffset returns the byte index of the n-th rune, counting each invalid
// byte as one rune as the lexer does.
func byteOffset(s string, n int) int {
	i := 0
	for ; n > 0 && i < len(s); n-- {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return i
}
