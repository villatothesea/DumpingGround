//go:build !windows

package uninstall

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
