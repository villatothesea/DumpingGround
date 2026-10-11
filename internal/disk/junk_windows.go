//go:build windows

package disk

import (
	"fmt"
	"os"
	"path/filepath"
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
	junkShell32             = syscall.NewLazyDLL("shell32.dll")
	junkOle32               = syscall.NewLazyDLL("ole32.dll")
	procSHQueryRecycle      = junkShell32.NewProc("SHQueryRecycleBinW")
	procSHEmptyRecycle      = junkShell32.NewProc("SHEmptyRecycleBinW")
	procSHGetKnownFolder    = junkShell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree       = junkOle32.NewProc("CoTaskMemFree")
)

// folderIDDownloads is FOLDERID_Downloads — resolves the real folder
// even when the user redirected it off C:.
var folderIDDownloads = syscall.GUID{
	Data1: 0x374DE290, Data2: 0x123F, Data3: 0x4565,
	Data4: [8]byte{0x91, 0x64, 0x39, 0xC4, 0x92, 0x5E, 0x46, 0x7B},
}

// DownloadsDir returns the user's Downloads folder via the shell's
// known-folder API, falling back to the profile default.
func DownloadsDir() string {
	var p *uint16
	hr, _, _ := procSHGetKnownFolder.Call(
		uintptr(unsafe.Pointer(&folderIDDownloads)), 0, 0,
		uintptr(unsafe.Pointer(&p)))
	if int32(hr) >= 0 && p != nil {
		defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
		n := 0
		for q := p; *q != 0; q = (*uint16)(unsafe.Add(unsafe.Pointer(q), 2)) {
			n++
		}
		return syscall.UTF16ToString(unsafe.Slice(p, n))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

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
