//go:build !windows

package disk

import "os"

func allocation(_ string, logical int64) (uint64, bool) { return uint64(max(logical, 0)), false }
func measureSpace(path string, logical int64) (uint64, bool, string) {
	v, k := allocation(path, logical)
	return v, k, ""
}
func skipEntry(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }
