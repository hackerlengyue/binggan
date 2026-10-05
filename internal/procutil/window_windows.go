package procutil

import (
	"os/exec"
	"syscall"
)

// HideWindow keeps FFmpeg and ffprobe from opening a console in the desktop app.
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
