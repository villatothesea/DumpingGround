//go:build !windows

package disk

func allocation(_ string, logical int64) uint64 {
	if logical < 0 {
		return 0
	}
	return uint64(logical)
}
