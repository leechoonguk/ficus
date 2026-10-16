//go:build windows

package engine

import (
	"encoding/binary"
	"os"
)

// helpRunnable reports a console program (PE subsystem CUI), the only kind
// --help may run. A GUI program opens its window whatever its arguments,
// and a batch file (.bat, .cmd, found through PATHEXT) is a script.
func helpRunnable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var dos [64]byte
	if _, err := f.ReadAt(dos[:], 0); err != nil || dos[0] != 'M' || dos[1] != 'Z' {
		return false
	}
	// "PE\0\0", the 20-byte COFF header, then the optional header, whose
	// Subsystem field is at offset 68 in both PE32 and PE32+.
	pe := int64(binary.LittleEndian.Uint32(dos[0x3c:]))
	var hdr [4 + 20 + 70]byte
	if _, err := f.ReadAt(hdr[:], pe); err != nil || string(hdr[:4]) != "PE\x00\x00" {
		return false
	}
	const subsystemConsole = 3
	return binary.LittleEndian.Uint16(hdr[4+20+68:]) == subsystemConsole
}
