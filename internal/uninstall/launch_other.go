//go:build !windows

package uninstall

import (
	"fmt"
	"os/exec"
)

// Process wraps the launched uninstaller off-Windows.
type Process struct{ cmd *exec.Cmd }

// Wait blocks until the process exits.
func (p *Process) Wait() error { return p.cmd.Wait() }

func hideWindow(cmd *exec.Cmd) {}

// launch runs the uninstall command directly — there is no shell
// elevation or quoting contract to honor outside Windows.
func launch(e *Entry) (*Process, error) {
	exe, args := SplitCmdline(e.Uninstall)
	if exe == "" {
		return nil, fmt.Errorf("no uninstall command")
	}
	cmd := exec.Command(exe, args...)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Process{cmd: cmd}, nil
}
