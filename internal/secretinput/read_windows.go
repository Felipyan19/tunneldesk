//go:build windows

package secretinput

import (
	"os/exec"
	"strings"
)

func Read() (string, error) {
	script := `$s=Read-Host -AsSecureString; $b=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($s); try {[Runtime.InteropServices.Marshal]::PtrToStringBSTR($b)} finally {[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($b)}`
	output, err := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
