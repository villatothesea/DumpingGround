//go:build !windows

package uninstall

import (
	"errors"
	"io"
	"os"
)

// RemoveItems on non-Windows builds only handles filesystem items —
// there is no registry. Dirs are deleted outright (no platform trash
// here); callers on Windows get the reversible path instead.
func RemoveItems(items []Item, backup io.Writer) []error {
	errs := make([]error, len(items))
	for i, it := range items {
		switch it.Kind {
		case KindDir:
			errs[i] = os.RemoveAll(it.Path)
		default:
			errs[i] = errors.New("registry removal is Windows-only")
		}
	}
	return errs
}
