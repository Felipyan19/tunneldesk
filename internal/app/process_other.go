//go:build !windows

package app

import "os/exec"

func hideRunnerWindow(*exec.Cmd) {}
