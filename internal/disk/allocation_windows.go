//go:build windows

package disk

import (
	"os"
	"syscall"
	"unsafe"
)

var fileInfoEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandleEx")

type standardInfo struct {
	AllocationSize, EndOfFile int64
	Links                     uint32
	DeletePending, Directory  byte
	_                         [2]byte
}

func allocation(path string, logical int64) (uint64, bool) {
	value, known, _ := measureSpace(path, logical)
	return value, known
}
func measureSpace(path string, logical int64) (uint64, bool, string) {
	fallback := uint64(max(logical, 0))
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fallback, false, ""
	}
	const readAttributes = 0x80
	const openReparsePoint = 0x00200000
	h, err := syscall.CreateFile(name, readAttributes, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, openReparsePoint, 0)
	if err != nil {
		return fallback, false, ""
	}
	defer syscall.CloseHandle(h)
	var info standardInfo
	ok, _, _ := fileInfoEx.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if ok == 0 {
		return fallback, false, ""
	}
	identity := ""
	if info.Links > 1 {
		var id syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(h, &id) == nil {
			var key [12]byte
			values := [3]uint32{id.VolumeSerialNumber, id.FileIndexHigh, id.FileIndexLow}
			for i, n := range values {
				for j := 0; j < 4; j++ {
					key[i*4+j] = byte(n >> uint(j*8))
				}
			}
			identity = string(key[:])
		}
	}
	return uint64(max(info.AllocationSize, 0)), true, identity
}
func skipEntry(info os.FileInfo) bool { return skipAttrs(info) != 0 }

// skipAttrs returns the attribute bits that make an entry uncountable:
// 0x400 for reparse points (junctions, symlinks and anything irregular),
// the offline/recall flags for cloud placeholders.
func skipAttrs(info os.FileInfo) uint32 {
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return 0x400
	}
	if attr, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		const reparse = 0x400
		const offline = 0x1000
		const recallOnOpen = 0x40000
		const recallOnData = 0x400000
		return attr.FileAttributes & (reparse | offline | recallOnOpen | recallOnData)
	}
	return 0
}
