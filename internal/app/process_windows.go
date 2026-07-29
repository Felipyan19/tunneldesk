//go:build windows

package app

import (
	"os/exec"
	"syscall"
)

func hideRunnerWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x08000000,
		HideWindow:    true,
	}
}
