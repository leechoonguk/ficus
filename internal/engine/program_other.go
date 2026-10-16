//go:build !windows

package engine

import "os"

// helpRunnable reports a program --help may run: not a script (a file
// starting with "#!"), which may ignore the flag and do its work instead.
func helpRunnable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 2)
	n, _ := f.Read(head)
	return n != 2 || string(head) != "#!"
}
