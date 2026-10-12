//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach makes cmd outlive the shell's job control: its own session, so a
// Ctrl-C at the prompt does not reach it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
