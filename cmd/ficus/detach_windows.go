//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detach runs cmd outside the shell's process group, on a console of its own
// that has no window. Its children inherit that console: a process without
// one (DETACHED_PROCESS) would make every console program it starts
// (ficus-carapace, `cmd --help`) open a new, visible console window.
func detach(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | syscall.CREATE_NEW_PROCESS_GROUP}
}
