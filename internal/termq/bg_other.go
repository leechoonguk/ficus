//go:build !unix

package termq

import "time"

// Background is not implemented here: Windows consoles do not answer OSC 11
// queries through the console API.
func Background(timeout time.Duration) (light, ok bool) {
	return false, false
}
