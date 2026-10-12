package engine

import (
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// Match is a candidate that matched the pattern.
type Match struct {
	Item
	// Positions are rune offsets into Item.Label() to highlight.
	Positions []int
	prefix    bool // starts with the pattern (case-folded unless it has capitals)
	exact     bool // starts with the pattern exactly as typed
	score     int
}

var initAlgo = sync.OnceFunc(func() { algo.Init("default") })

// Filter ranks items against pattern with fzf's FuzzyMatchV2 (smart case,
// diacritics-insensitive). Prefix matches rank first, as a completion menu
// should list what the user is literally typing before fuzzy hits, and among
// them those in the typed case (-l before -L), then recently accepted ones
// (recent maps values to their most-recent-first rank, see LoadRecent). An
// empty pattern keeps the backend's order.
func Filter(items []Item, fetchPrefix, pattern string, recent map[string]int) []Match {
	initAlgo()
	caseSensitive := strings.IndexFunc(pattern, unicode.IsUpper) >= 0
	query := pattern
	if !caseSensitive {
		query = strings.ToLower(pattern)
	}
	pat := algo.NormalizeRunes([]rune(query))
	slab := util.MakeSlab(100*1024, 2048)

	matches := make([]Match, 0, len(items))
	for _, it := range items {
		// Match the part of the value after the fetched prefix: that is what
		// the pattern was typed against.
		text := strings.TrimPrefix(it.Value, fetchPrefix)
		m := Match{Item: it, prefix: true}
		if len(pat) > 0 {
			chars := util.ToChars([]byte(text))
			res, _ := algo.FuzzyMatchV2(caseSensitive, true, true, &chars, pat, false, slab)
			if res.Start < 0 {
				continue
			}
			m.score = res.Score
			m.prefix = hasPrefixFold(text, pattern, caseSensitive)
			m.exact = strings.HasPrefix(text, pattern)
			m.Positions = highlight(it.Label(), pat, caseSensitive, slab)
		}
		matches = append(matches, m)
	}
	if len(pat) == 0 {
		return matches
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.prefix != b.prefix {
			return a.prefix
		}
		if a.exact != b.exact {
			return a.exact
		}
		ra, aok := recent[a.Value]
		rb, bok := recent[b.Value]
		if aok != bok {
			return aok
		}
		if aok && ra != rb {
			return ra < rb
		}
		if a.score != b.score {
			return a.score > b.score
		}
		return len(a.Value) < len(b.Value)
	})
	return matches
}

// CommonPrefix returns the longest prefix (case-sensitive) that every
// match continues, after fetchPrefix, when all of them are prefix matches,
// there are two or more and it is longer than pattern: what Tab can insert
// without choosing between them.
func CommonPrefix(matches []Match, fetchPrefix, pattern string) (string, bool) {
	if len(matches) < 2 {
		return "", false
	}
	lcp := strings.TrimPrefix(matches[0].Value, fetchPrefix)
	for _, m := range matches {
		if !m.prefix {
			return "", false
		}
		v := strings.TrimPrefix(m.Value, fetchPrefix)
		n := 0
		for n < len(lcp) && n < len(v) && lcp[n] == v[n] {
			n++
		}
		lcp = lcp[:n]
	}
	// Never split a UTF-8 sequence.
	for len(lcp) > 0 && !utf8.ValidString(lcp) {
		lcp = lcp[:len(lcp)-1]
	}
	if len(lcp) <= len(pattern) {
		return "", false
	}
	return lcp, true
}

func hasPrefixFold(s, prefix string, caseSensitive bool) bool {
	if caseSensitive {
		return strings.HasPrefix(s, prefix)
	}
	return strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
}

// highlight returns the matched rune positions within the label, which may
// differ from the matched text (e.g. a basename display of a path value).
func highlight(label string, pat []rune, caseSensitive bool, slab *util.Slab) []int {
	chars := util.ToChars([]byte(label))
	res, pos := algo.FuzzyMatchV2(caseSensitive, true, true, &chars, pat, true, slab)
	if res.Start < 0 || pos == nil {
		return nil
	}
	p := append([]int(nil), (*pos)...)
	sort.Ints(p)
	return p
}
