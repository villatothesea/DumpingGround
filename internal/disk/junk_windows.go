//go:build windows

package disk

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// junkReparse reports whether a walked entry is any kind of reparse
// point — symlink, junction or mount point — so cleanup never follows
// or removes one. ModeSymlink alone misses junctions on Windows.
func junkReparse(e os.DirEntry) bool {
	if e.Type()&os.ModeSymlink != 0 {
		return true
	}
	info, err := e.Info()
	if err != nil {
		return true // cannot inspect — do not touch
	}
	if w, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return w.FileAttributes&0x400 != 0 // FILE_ATTRIBUTE_REPARSE_POINT
	}
	return false
}

var (
	junkShell32          = syscall.NewLazyDLL("shell32.dll")
	procSHQueryRecycle   = junkShell32.NewProc("SHQueryRecycleBinW")
	procSHEmptyRecycle   = junkShell32.NewProc("SHEmptyRecycleBinW")
)

// shQueryRBInfo mirrors SHQUERYRBINFO.
type shQueryRBInfo struct {
	size  int32
	bytes int64
	items int64
}

const sherbNoUI = 0x1 | 0x2 | 0x4 // no confirmation, no progress, no sound

// RecycleBinStats returns the bin's total size and item count across all
// drives — the same SHQueryRecycleBin the cleanup tools use.
func RecycleBinStats() (bytes, items uint64) {
	var q shQueryRBInfo
	q.size = int32(unsafe.Sizeof(q))
	hr, _, _ := procSHQueryRecycle.Call(0, uintptr(unsafe.Pointer(&q)))
	if int32(hr) < 0 {
		return 0, 0
	}
	return uint64(q.bytes), uint64(q.items)
}

// EmptyRecycleBin empties every drive's bin through the shell — proper
// bookkeeping, unlike deleting $Recycle.Bin contents by hand.
func EmptyRecycleBin() error {
	hr, _, _ := procSHEmptyRecycle.Call(0, 0, sherbNoUI)
	if int32(hr) < 0 {
		return fmt.Errorf("SHEmptyRecycleBin: 0x%08x", uint32(hr))
	}
	return nil
}
