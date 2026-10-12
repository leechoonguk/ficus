package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// Recently accepted values are kept per command (the key "" holds command
// names), most recent first, so that among equally good matches the menu
// lists what the user picks.
const (
	recentFile   = "recent.json"
	recentKeys   = 200
	recentValues = 50
)

type recentStore struct {
	Keys []recentEntry `json:"keys"`
}

type recentEntry struct {
	Cmd    string   `json:"cmd"`
	Values []string `json:"values"`
}

// recentKey is the history a context ranks by: command names in command
// position, the command's own values in argument position, none otherwise.
func recentKey(c *Context) (string, bool) {
	switch c.Kind {
	case KindCommand:
		return "", true
	case KindArgument:
		return c.Words[0], true
	}
	return "", false
}

func readRecent(cfg Config) recentStore {
	var st recentStore
	if data, err := os.ReadFile(filepath.Join(cfg.CacheDir, recentFile)); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

// LoadRecent returns the recently accepted values for c with their rank
// (0 = most recent), for Filter.
func LoadRecent(cfg Config, c *Context) map[string]int {
	key, ok := recentKey(c)
	if !ok || cfg.CacheDir == "" {
		return nil
	}
	for _, e := range readRecent(cfg).Keys {
		if e.Cmd == key {
			m := make(map[string]int, len(e.Values))
			for i, v := range e.Values {
				m[v] = i
			}
			return m
		}
	}
	return nil
}

// RecordRecent moves value to the front of c's history. Concurrent shells
// may race; the last writer wins, which only loses a ranking hint.
func RecordRecent(cfg Config, c *Context, value string) error {
	key, ok := recentKey(c)
	if !ok || cfg.CacheDir == "" || value == "" {
		return nil
	}
	st := readRecent(cfg)
	var values []string
	for i, e := range st.Keys {
		if e.Cmd == key {
			values = e.Values
			st.Keys = slices.Delete(st.Keys, i, i+1)
			break
		}
	}
	values = slices.DeleteFunc(values, func(v string) bool { return v == value })
	values = append([]string{value}, values...)
	if len(values) > recentValues {
		values = values[:recentValues]
	}
	st.Keys = append([]recentEntry{{Cmd: key, Values: values}}, st.Keys...)
	if len(st.Keys) > recentKeys {
		st.Keys = st.Keys[:recentKeys]
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return writeAtomic(cfg.CacheDir, filepath.Join(cfg.CacheDir, recentFile), data)
}
