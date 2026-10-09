//go:build windows

package process

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var queryInformation = syscall.NewLazyDLL("ntdll.dll").NewProc("NtQueryInformationProcess")
var handleCount = kernel32.NewProc("GetProcessHandleCount")
var priorityClass = kernel32.NewProc("GetPriorityClass")

func (c *windowsCollector) Properties(pid uint32) (Info, error) {
	info := Info{PID: pid}
	h, _, err := procOpen.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return info, err
	}
	defer procClose.Call(h)
	info.Path = queryPath(h)
	handleCount.Call(h, uintptr(unsafe.Pointer(&info.Handles)))
	p, _, _ := priorityClass.Call(h)
	info.Priority = uint32(p)
	var token syscall.Token
	if syscall.OpenProcessToken(syscall.Handle(h), syscall.TOKEN_QUERY, &token) == nil {
		defer token.Close()
		if user, e := token.GetTokenUser(); e == nil {
			if name, domain, _, e := user.User.Sid.LookupAccount(""); e == nil {
				info.User = name
				if domain != "" {
					info.User = domain + "\\" + name
				}
			}
		}
		var elevated uint32
		var length uint32
		if syscall.GetTokenInformation(token, 20, (*byte)(unsafe.Pointer(&elevated)), 4, &length) == nil {
			info.Elevated = elevated != 0
		}
	}
	// Class 60 returns a UNICODE_STRING followed by a command line in this
	// caller-owned buffer. Validate its pointer and length before decoding.
	buf := make([]byte, 128*1024)
	var needed uint32
	status, _, _ := queryInformation.Call(h, 60, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&needed)))
	if uint32(status) == 0 {
		type unicodeString struct {
			Length, Maximum uint16
			Buffer          uintptr
		}
		us := *(*unicodeString)(unsafe.Pointer(&buf[0]))
		base := uintptr(unsafe.Pointer(&buf[0]))
		offset := us.Buffer - base
		if us.Buffer >= base && offset <= uintptr(len(buf)) && uintptr(us.Length) <= uintptr(len(buf))-offset && us.Length%2 == 0 {
			chars := make([]uint16, int(us.Length)/2)
			for i := range chars {
				chars[i] = binary.LittleEndian.Uint16(buf[int(offset)+i*2:])
			}
			info.Command = syscall.UTF16ToString(chars)
		}
	} else {
		info.Access = fmt.Sprintf("Command line unavailable (NTSTATUS 0x%08x)", uint32(status))
	}
	runtime.KeepAlive(buf)
	return info, nil
}
