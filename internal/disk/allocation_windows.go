//go:build windows

package disk

import (
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getCompressedFileSize = kernel32.NewProc("GetCompressedFileSizeW")
)

func allocation(path string, logical int64) uint64 {
	fallback := uint64(0)
	if logical > 0 {
		fallback = uint64(logical)
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fallback
	}
	var high uint32
	low, _, _ := getCompressedFileSize.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&high)))
	if low == ^uintptr(0) && syscall.GetLastError() != nil {
		return fallback
	}
	return uint64(high)<<32 | uint64(uint32(low))
}
