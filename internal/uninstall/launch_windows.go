//go:build windows

package uninstall

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps console-subsystem uninstall commands (rare, but
// batch-driven uninstallers exist) from flashing a black cmd window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
