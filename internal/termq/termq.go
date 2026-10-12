// Package termq asks the terminal about itself.
package termq

import (
	"regexp"
	"strconv"
)

var (
	oscBackground = regexp.MustCompile(`\x1b\]11;rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})(?:\x07|\x1b\\)`)
	deviceAttrs   = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
)

// parseReply reads the replies to an OSC 11 background color query and a
// DA1 (primary device attributes) query. Each color channel is scaled to
// 0-1 from its 1-4 hex digits.
func parseReply(buf []byte) (r, g, b float64, gotOSC, gotDA bool) {
	gotDA = deviceAttrs.Match(buf)
	m := oscBackground.FindSubmatch(buf)
	if m == nil {
		return 0, 0, 0, false, gotDA
	}
	var ch [3]float64
	for i := range ch {
		v, _ := strconv.ParseUint(string(m[i+1]), 16, 64)
		ch[i] = float64(v) / float64(uint64(1)<<(4*len(m[i+1]))-1)
	}
	return ch[0], ch[1], ch[2], true, gotDA
}

// isLight reports a light color by its relative luminance.
func isLight(r, g, b float64) bool {
	return 0.2126*r+0.7152*g+0.0722*b > 0.5
}
