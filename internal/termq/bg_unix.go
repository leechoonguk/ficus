//go:build unix

package termq

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Background asks the terminal for its background color (OSC 11) and
// reports whether it is light. ok is false when the terminal does not
// answer within timeout. The DA1 query sent after it is answered by every
// terminal, so a terminal without OSC 11 support costs no wait.
func Background(timeout time.Duration) (light, ok bool) {
	if os.Getenv("TERM") == "dumb" {
		return false, false
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, false
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return false, false
	}
	defer term.Restore(fd, state)
	if _, err := tty.WriteString("\x1b]11;?\x1b\\\x1b[c"); err != nil {
		return false, false
	}

	var buf []byte
	chunk := make([]byte, 256)
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(left.Milliseconds())+1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n == 0 {
			break
		}
		m, err := unix.Read(fd, chunk)
		if m > 0 {
			buf = append(buf, chunk[:m]...)
		}
		if err != nil || m <= 0 {
			break
		}
		if _, _, _, _, gotDA := parseReply(buf); gotDA {
			break // the reply to the last query: nothing more is coming
		}
	}
	r, g, b, gotOSC, _ := parseReply(buf)
	if !gotOSC {
		return false, false
	}
	return isLight(r, g, b), true
}
