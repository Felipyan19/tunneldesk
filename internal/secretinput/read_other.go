//go:build !windows

package secretinput

import (
	"bufio"
	"os"
	"strings"
)

func Read() (string, error) {
	value, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(value), err
}
