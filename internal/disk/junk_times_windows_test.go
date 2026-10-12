//go:build windows

package disk

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// setAllFileTimes ages a fixture file under every clock the age rule
// reads — Chtimes alone leaves creation time at "now", which is
// exactly the case junkNewestTime guards.
func setAllFileTimes(t *testing.T, path string, tm time.Time) {
	t.Helper()
	if err := os.Chtimes(path, tm, tm); err != nil {
		t.Fatal(err)
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(p, syscall.FILE_WRITE_ATTRIBUTES,
		0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(h)
	ft := syscall.NsecToFiletime(tm.UnixNano())
	if err := syscall.SetFileTime(h, &ft, &ft, &ft); err != nil {
		t.Fatal(err)
	}
}
