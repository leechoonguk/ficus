package termq

import "testing"

func TestParseReply(t *testing.T) {
	r, g, b, osc, da := parseReply([]byte("\x1b]11;rgb:ffff/ffff/ffff\x1b\\\x1b[?62;c"))
	if !osc || !da || !isLight(r, g, b) {
		t.Errorf("white background: rgb %v %v %v osc %v da %v", r, g, b, osc, da)
	}
	r, g, b, osc, _ = parseReply([]byte("\x1b]11;rgb:00/00/00\x07"))
	if !osc || isLight(r, g, b) {
		t.Errorf("black background with BEL: rgb %v %v %v osc %v", r, g, b, osc)
	}
	if _, _, _, osc, da := parseReply([]byte("\x1b[?1;2c")); osc || !da {
		t.Errorf("DA only: osc %v da %v", osc, da)
	}
}
