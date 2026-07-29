//go:build windows

package openvpn

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x08000000,
		HideWindow:    true,
	}
}

func processMatches(pid int, expectedPath string) bool {
	if pid <= 0 || expectedPath == "" {
		return false
	}
	script := `(Get-CimInstance Win32_Process -Filter "ProcessId = ` + strconv.Itoa(pid) + `").ExecutablePath`
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return false
	}
	actual := strings.TrimSpace(string(out))
	if actual == "" {
		return false
	}
	expected, err := filepath.Abs(expectedPath)
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(actual), filepath.Clean(expected))
}

func gracefulStop(pid int, timeout time.Duration) error {
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid)).Run()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return forceStop(pid)
}

func forceStop(pid int) error {
	return exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
}

func processExists(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH").Output()
	return err == nil && strings.Contains(string(out), strconv.Itoa(pid))
}

func replaceFile(source, target string) error {
	_ = syscall.DeleteFile(syscall.StringToUTF16Ptr(target))
	return syscall.MoveFile(syscall.StringToUTF16Ptr(source), syscall.StringToUTF16Ptr(target))
}
