//go:build !windows

package procutil

import "os/exec"

func HideWindow(*exec.Cmd) {}
