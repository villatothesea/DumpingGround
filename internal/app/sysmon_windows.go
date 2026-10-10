//go:build windows

package app

import (
	"encoding/binary"
	"syscall"
	"time"
	"unsafe"
)

var (
	procGetSystemTimes       = syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	procGetIfTable           = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetIfTable")
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
	total, busy           uint64
	cpu                   float64
	memLoad               uint32
	memTotal              uint64
	memUsed               uint64
	netIn, netOut         uint64
	netInRate, netOutRate uint64
	netWhen               time.Time
}

func (s *systemUsage) sample() {
	var idle, kernel, user [2]uint32
	r, _, _ := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r != 0 {
		// GetSystemTimes counts idle inside its kernel figure: busy work
		// is kernel + user - idle against a total of kernel + user.
		i := uint64(idle[0]) | uint64(idle[1])<<32
		tot := (uint64(kernel[0]) | uint64(kernel[1])<<32) + (uint64(user[0]) | uint64(user[1])<<32)
		b := tot - i
		if d := tot - s.total; d > 0 && s.total > 0 {
			s.cpu = float64(b-s.busy) / float64(d) * 100
		}
		s.total, s.busy = tot, b
	}
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r != 0 {
		s.memLoad = m.MemoryLoad
		s.memTotal = m.TotalPhys
		s.memUsed = m.TotalPhys - m.AvailPhys
	}
	// Aggregate byte counters across interfaces; loopback included is fine
	// for a coarse rate meter.
	var size uint32
	procGetIfTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size > 0 {
		buf := make([]byte, int(size))
		if r, _, _ := procGetIfTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0); r == 0 {
			const rowLen = 860 // MIB_IFROW
			n := int(binary.LittleEndian.Uint32(buf))
			var in, out uint64
			for i := 0; i < n && 4+i*rowLen+rowLen <= len(buf); i++ {
				row := buf[4+i*rowLen:]
				in += uint64(binary.LittleEndian.Uint32(row[552:]))  // dwInOctets
				out += uint64(binary.LittleEndian.Uint32(row[576:])) // dwOutOctets
			}
			now := time.Now()
			if !s.netWhen.IsZero() {
				dt := now.Sub(s.netWhen).Seconds()
				if dt > 0 {
					s.netInRate = uint64(float64(in-s.netIn) / dt)
					s.netOutRate = uint64(float64(out-s.netOut) / dt)
				}
			}
			s.netIn, s.netOut, s.netWhen = in, out, now
		}
	}
}
