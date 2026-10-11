//go:build !windows

package uninstall

import (
	"fmt"
	"os"
	"syscall"
)

// Non-Windows builds keep the same shape so the UI compiles; the scan
// looks at the conventional XDG-style dirs instead.
func appRoots() []string {
	var roots []string
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		roots = append(roots, v)
	} else if h, _ := os.UserHomeDir(); h != "" {
		roots = append(roots, h+"/.config")
	}
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		roots = append(roots, v)
	}
	return roots
}

func shortcutRoots() []string {
	if h, err := os.UserHomeDir(); err == nil {
		return []string{h + "/.local/share/applications"}
	}
	return nil
}

func isReparse(e os.DirEntry) bool {
	return e.Type()&os.ModeSymlink != 0
}

func regFootprint(e *Entry) []Item { return nil }

// Gone has no registry to consult off-Windows.
func (e *Entry) Gone() bool { return true }

// OpenInRegedit is meaningless off-Windows.
func (e *Entry) OpenInRegedit() error {
	return fmt.Errorf("regedit is Windows-only")
}

var _ = syscall.Errno(0)
