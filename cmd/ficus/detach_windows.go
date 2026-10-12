//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detach runs cmd without a console and outside the shell's process group.
func detach(cmd *exec.Cmd) {
	const detachedProcess = 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP}
}
