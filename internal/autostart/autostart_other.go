//go:build !windows

package autostart

import "errors"

var unsupported = errors.New("Windows startup integration is available only on Windows")

func Enable(string) error  { return unsupported }
func Disable(string) error { return unsupported }
