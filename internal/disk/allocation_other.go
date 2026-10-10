//go:build !windows

package disk

import "os"

func allocation(_ string, logical int64) (uint64, bool) { return uint64(max(logical, 0)), false }
func measureSpace(path string, logical int64) (uint64, bool, string) {
	v, k := allocation(path, logical)
	return v, k, ""
}
func skipEntry(info os.FileInfo) bool { return skipAttrs(info) != 0 }

// skipAttrs treats symlinks as reparse points; other platforms have no
// cloud-placeholder flags to report.
func skipAttrs(info os.FileInfo) uint32 {
	if info.Mode()&os.ModeSymlink != 0 {
		return 0x400
	}
	return 0
}
