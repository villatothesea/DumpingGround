//go:build windows

package process

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	systemExtendedHandleInformation = 64
	objectNameInformation           = 1
	objectTypeInformation           = 2
	processDupHandle                = 0x40
	duplicateSameAccess             = 2
	handleInfoMax                   = 2_000_000
)

type systemHandleEntry struct {
	Object, PID, Handle uintptr
	Granted             uint32
	Creator             uint16
	Type                uint16
	Attributes          uint32
	Reserved            uint32
}
type unicodeString struct {
	Length, Maximum uint16
	Buffer          *uint16
}

var (
	ntQuerySystemInformation = syscall.NewLazyDLL("ntdll.dll").NewProc("NtQuerySystemInformation")
	ntQueryObject            = syscall.NewLazyDLL("ntdll.dll").NewProc("NtQueryObject")
	duplicateHandle          = kernel32.NewProc("DuplicateHandle")
	getCurrentProcess        = kernel32.NewProc("GetCurrentProcess")
)

func ntStatusError(status uintptr) error {
	if status == 0 {
		return nil
	}
	return syscall.Errno(status & 0xffff)
}
func querySystemHandles() ([]systemHandleEntry, error) {
	size := uint32(4 << 20)
	for size <= uint32(512<<20) {
		buf := make([]byte, size)
		var need uint32
		status, _, _ := ntQuerySystemInformation.Call(systemExtendedHandleInformation, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&need)))
		if status == 0 {
			if len(buf) < 16 {
				return nil, errors.New("short handle information")
			}
			count := *(*uintptr)(unsafe.Pointer(&buf[0]))
			if count > handleInfoMax {
				return nil, errors.New("handle table too large")
			}
			base := unsafe.Pointer(&buf[16])
			step := unsafe.Sizeof(systemHandleEntry{})
			if uint64(count)*uint64(step) > uint64(len(buf)-16) {
				return nil, errors.New("invalid handle table")
			}
			out := make([]systemHandleEntry, 0, int(count))
			for i := uintptr(0); i < count; i++ {
				out = append(out, *(*systemHandleEntry)(unsafe.Add(base, i*step)))
			}
			runtime.KeepAlive(buf)
			return out, nil
		}
		if status != 0xc0000004 && status != 0xc0000023 {
			return nil, ntStatusError(status)
		}
		if need > size {
			size = need + 1
		} else {
			size *= 2
		}
	}
	return nil, errors.New("handle table exceeds safety limit")
}
func readUnicode(p unsafe.Pointer, max int) string {
	if p == nil {
		return ""
	}
	u := *(*unicodeString)(p)
	if u.Length == 0 || u.Length%2 != 0 || int(u.Length) > max {
		return ""
	}
	raw := unsafe.Slice(u.Buffer, int(u.Length)/2)
	return syscall.UTF16ToString(raw)
}
func queryObjectString(handle uintptr, class uint32) string {
	size := uint32(4096)
	for i := 0; i < 3; i++ {
		buf := make([]byte, size)
		var need uint32
		status, _, _ := ntQueryObject.Call(handle, uintptr(class), uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&need)))
		if status == 0 {
			result := readUnicode(unsafe.Pointer(&buf[0]), 64<<10)
			runtime.KeepAlive(buf)
			return result
		}
		if status != 0xc0000004 {
			return ""
		}
		if need > size {
			size = need + 1
		} else {
			size *= 2
		}
	}
	return ""
}
func queryHandleStrings(handle uintptr) (string, string) {
	return queryObjectString(handle, objectTypeInformation), queryObjectString(handle, objectNameInformation)
}
func queryHandleStringsTimed(handle uintptr) (typ, name string, ok bool) {
	done := make(chan struct{})
	go func() { defer procClose.Call(handle); typ, name = queryHandleStrings(handle); close(done) }()
	select {
	case <-done:
		return typ, name, true
	case <-time.After(150 * time.Millisecond):
		return "", "", false
	}
}

func (c *windowsCollector) SearchHandles(pid uint32, query string, limit int) ([]Handle, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	entries, err := querySystemHandles()
	if err != nil {
		return nil, err
	}
	current, _, _ := getCurrentProcess.Call()
	needle := strings.ToLower(query)
	out := make([]Handle, 0, minInt(limit, 128))
	skipped := 0
	for _, entry := range entries {
		if entry.PID == 0 || (pid != 0 && uint32(entry.PID) != pid) {
			continue
		}
		source, _, _ := procOpen.Call(processDupHandle, 0, entry.PID)
		if source == 0 {
			skipped++
			continue
		}
		var dup uintptr
		ok, _, _ := duplicateHandle.Call(source, entry.Handle, current, uintptr(unsafe.Pointer(&dup)), 0, 0, duplicateSameAccess)
		procClose.Call(source)
		if ok == 0 || dup == 0 {
			skipped++
			continue
		}
		typ, name, finished := queryHandleStringsTimed(dup)
		if !finished {
			skipped++
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(typ+" "+name), needle) {
			continue
		}
		out = append(out, Handle{PID: uint32(entry.PID), Value: entry.Handle, Type: typ, Name: name, Access: entry.Granted, Attributes: entry.Attributes})
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 && skipped > 0 {
		return out, errors.New("no matching handles; access denied for some processes")
	}
	return out, nil
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
