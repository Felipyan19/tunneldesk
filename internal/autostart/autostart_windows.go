//go:build windows

package autostart

import (
	"fmt"
	"os"
	"os/exec"
)

const registryKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func Enable(profile string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	name := "TunnelDesk-" + profile
	command := fmt.Sprintf(`"%s" autoconnect --%s`, executable, profile)
	return exec.Command("reg", "add", registryKey, "/v", name, "/t", "REG_SZ", "/d", command, "/f").Run()
}

func Disable(profile string) error {
	name := "TunnelDesk-" + profile
	return exec.Command("reg", "delete", registryKey, "/v", name, "/f").Run()
}
