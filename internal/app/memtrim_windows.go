//go:build windows

package app

import (
	"runtime/debug"
	"syscall"
)

var procSetProcessWorkingSetSize = syscall.NewLazyDLL("kernel32.dll").NewProc("SetProcessWorkingSetSize")

// trimMemory returns idle heap to the OS and asks Windows to page out
// whatever the process is not actively touching. Pages come back on
// demand, so nothing is lost — only the resident set shrinks while the
// app sits minimized.
func trimMemory() {
	debug.FreeOSMemory()
	// (HANDLE)-1, (SIZE_T)-1, (SIZE_T)-1: trim to the minimum possible.
	procSetProcessWorkingSetSize.Call(^uintptr(0), ^uintptr(0), ^uintptr(0))
}
