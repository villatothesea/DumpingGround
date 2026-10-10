//go:build !windows

package disk

import "os"

// RecycleFileOrDir has no shell trash concept here; fall back to a plain
// removal so the deletion pipeline stays testable off Windows.
func RecycleFileOrDir(path string) error {
	return os.RemoveAll(path)
}
