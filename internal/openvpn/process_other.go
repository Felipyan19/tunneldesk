//go:build !windows

package openvpn

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func prepareProcess(*exec.Cmd) {}

func processMatches(pid int, _ string) bool {
	process, err := os.FindProcess(pid)
	return err == nil && process.Signal(syscall.Signal(0)) == nil
}

func gracefulStop(pid int, timeout time.Duration) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = process.Signal(os.Interrupt)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processMatches(pid, "") {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return process.Kill()
}

func forceStop(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
