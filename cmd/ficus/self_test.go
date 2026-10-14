package main

import (
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"ficus/internal/engine"
)

// engine.SelfCommands, what ficus completes for its own command line, names
// real subcommands and exactly the flags their flag sets define.
func TestSelfCommandsMatchCLI(t *testing.T) {
	for _, sc := range engine.SelfCommands {
		if sc.Name != "help" && !strings.Contains(usage, "ficus "+sc.Name) {
			t.Errorf("completion lists %q, which the usage does not", sc.Name)
		}
	}
	flagLine := regexp.MustCompile(`(?m)^  -(\S+)`)
	for _, name := range []string{"setup", "update"} {
		var want []string
		for _, sc := range engine.SelfCommands {
			if sc.Name == name {
				for _, f := range sc.Flags {
					want = append(want, f.Name)
				}
			}
		}
		var got []string
		for _, m := range flagLine.FindAllStringSubmatch(helpOutput(t, name), -1) {
			got = append(got, m[1])
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: flags %v, completion lists %v", name, got, want)
		}
	}
}

// helpOutput is what `ficus <name> -h` prints: the flag package writes the
// flag list to stderr.
func helpOutput(t *testing.T, name string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = w
	run([]string{name, "-h"}, io.Discard)
	os.Stderr = stderr
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}
