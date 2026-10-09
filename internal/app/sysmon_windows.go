//go:build windows

package app

import (
	"syscall"
	"unsafe"
)

var (
	procGetSystemTimes       = syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// systemUsage samples total CPU and physical memory. sampleSystem calls it
// at frame pace; cpu stays at the last reading when too little time passed.
type systemUsage struct {
	idle, busy uint64
	cpu        float64
	memLoad    uint32
	memTotal   uint64
	memUsed    uint64
}

func (s *systemUsage) sample() {
	var idle, kernel, user [2]uint32
	r, _, _ := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r != 0 {
		i := uint64(idle[0]) | uint64(idle[1])<<32
		b := (uint64(kernel[0]) | uint64(kernel[1])<<32) + (uint64(user[0]) | uint64(user[1])<<32)
		if d := i + b - s.idle - s.busy; d > 0 && s.busy > 0 {
			s.cpu = float64(b-s.busy) / float64(d) * 100
		}
		s.idle, s.busy = i, b
	}
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r != 0 {
		s.memLoad = m.MemoryLoad
		s.memTotal = m.TotalPhys
		s.memUsed = m.TotalPhys - m.AvailPhys
	}
}
