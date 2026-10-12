// Command ficus-carapace is the completion data backend of ficus: an
// unmodified build of carapace-bin from third_party/carapace-bin.
//
// It is a separate binary because carapace registers ~1000 completers in
// package init, which costs ~150ms per process. The per-keystroke ficus
// engine must not pay that; it only runs this binary on cache misses.
package main

import (
	"os"

	"github.com/carapace-sh/carapace-bin/cmd/carapace/cmd"
)

var version = "develop"

func main() {
	if err := cmd.Execute(version); err != nil {
		os.Exit(1)
	}
}
