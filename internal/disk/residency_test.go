//go:build windows

package disk

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

// FileStandardInfo.AllocationSize for resident-sized files decides whether
// MFT resident data should count as 0 allocated bytes.
func TestResidentFileAllocation(t *testing.T) {
	dir := t.TempDir()
	for _, size := range []int{1, 100, 700, 1500, 5000, 65536} {
		p := filepath.Join(dir, "f"+string(rune('a'+size/1024))+".bin")
		p = filepath.Join(dir, "f.bin")
		if err := os.WriteFile(p, make([]byte, size), 0644); err != nil {
			t.Fatal(err)
		}
		alloc, known, _ := measureSpace(p, int64(size))
		t.Logf("size=%d measured=%d known=%v", size, alloc, known)

		name, _ := syscall.UTF16PtrFromString(p)
		h, err := syscall.CreateFile(name, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
		if err != nil {
			t.Fatal(err)
		}
		var info standardInfo
		fileInfoEx.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
		syscall.CloseHandle(h)
		t.Logf("size=%d AllocationSize=%d EndOfFile=%d", size, info.AllocationSize, info.EndOfFile)
		os.Remove(p)
	}
}
