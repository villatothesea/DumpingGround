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
func RemoveItems(items []Item, backup io.Writer, protected []string, progress ...func(done, total int, path string)) []error {
	errs := make([]error, len(items))
	for i, it := range items {
		if !it.Registry() && disk.ProtectedPath(it.Path, protected) {
			errs[i] = errProtected
			continue
		}
		switch it.Kind {
		case KindDir:
			if it.ID != nil && !it.ID.Verify() {
				errs[i] = errPathChanged
				continue
			}
			errs[i] = os.RemoveAll(it.Path)
		default:
			errs[i] = errors.New("registry removal is Windows-only")
		}
	}
	return errs
}
