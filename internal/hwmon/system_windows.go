//go:build windows

package hwmon

import (
	"encoding/binary"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func loadOS(info *Info) {
	info.Host, _ = os.Hostname()
	info.User = os.Getenv("USERNAME")
	switch runtime.GOARCH {
	case "amd64":
		info.Arch = "64-bit (x64)"
	case "arm64":
		info.Arch = "64-bit (ARM64)"
	default:
		info.Arch = runtime.GOARCH
	}
	h, ok := regOpen(`SOFTWARE\Microsoft\Windows NT\CurrentVersion`)
	if !ok {
		return
	}
	defer syscall.RegCloseKey(h)
	info.OS = regStr(h, "ProductName")
	build, _ := strconv.Atoi(regStr(h, "CurrentBuildNumber"))
	// ProductName still says "Windows 10" on Windows 11 — the build number
	// is the reliable tell.
	if build >= 22000 {
		info.OS = strings.Replace(info.OS, "Windows 10", "Windows 11", 1)
	}
	info.OSVersion = regStr(h, "DisplayVersion")
	if info.OSVersion == "" {
		info.OSVersion = regStr(h, "ReleaseId")
	}
	info.OSBuild = strconv.Itoa(build)
	if ubr, ok := regDword(h, "UBR"); ok {
		info.OSBuild += "." + strconv.Itoa(int(ubr))
	}
}

func loadSMBIOS(info *Info) {
	const rsmb = 'R'<<24 | 'S'<<16 | 'M'<<8 | 'B'
	n, _, _ := procGetSystemFirmwareTable.Call(rsmb, 0, 0, 0)
	if n == 0 || n > 4<<20 {
		return
	}
	buf := make([]byte, n)
	if r, _, _ := procGetSystemFirmwareTable.Call(rsmb, 0, uintptr(unsafe.Pointer(&buf[0])), n); r == 0 || r > n {
		return
	}
	sm := parseSMBIOS(buf)
	info.Manufacturer, info.Model = sm.SysMfr, sm.SysProduct
	info.BIOSVendor, info.BIOSVersion, info.BIOSDate = sm.BIOSVendor, sm.BIOSVersion, sm.BIOSDate
	info.BoardVendor, info.BoardProduct = sm.BoardMfr, sm.BoardProduct
	info.Mem.Modules, info.Mem.Slots, info.Mem.MaxCapacity = sm.Modules, sm.Slots, sm.MaxCapacity
}

func loadCPU(info *Info) {
	if h, ok := regOpen(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`); ok {
		info.CPU.Name = regStr(h, "ProcessorNameString")
		info.CPU.Vendor = regStr(h, "VendorIdentifier")
		if mhz, ok := regDword(h, "~MHz"); ok {
			info.CPU.BaseMHz = int(mhz)
		}
		syscall.RegCloseKey(h)
	}
	n, _, _ := procGetActiveProcessorCount.Call(0xFFFF)
	info.CPU.Threads = int(n)
	if info.CPU.Threads == 0 {
		info.CPU.Threads = runtime.NumCPU()
	}
	// RelationAll: cores, sockets and every cache instance in one blob.
	var size uint32
	procGetLogicalProcInfoEx.Call(0xFFFF, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 1<<20 {
		return
	}
	buf := make([]byte, size)
	if r, _, _ := procGetLogicalProcInfoEx.Call(0xFFFF, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		return
	}
	for off := 0; off+8 <= int(size); {
		rel, rec := le32(buf[off:]), int(le32(buf[off+4:]))
		if rec < 8 || off+rec > int(size) {
			break
		}
		r := buf[off : off+rec]
		switch rel {
		case 0:
			info.CPU.Cores++
		case 3:
			info.CPU.Sockets++
		case 2:
			if rec >= 20 {
				sz := uint64(le32(r[12:]))
				switch r[8] {
				case 1:
					info.CPU.L1 += sz
				case 2:
					info.CPU.L2 += sz
				case 3:
					info.CPU.L3 += sz
				}
			}
		}
		off += rec
	}
}

type perfInfo struct {
	total, avail, cache, commit, limit, paged, nonpaged uint64
	handles, procs, threads                             uint32
}

func readPerfInfo() (pi perfInfo, ok bool) {
	var b [104]byte
	binary.LittleEndian.PutUint32(b[:], uint32(len(b)))
	if r, _, _ := procGetPerformanceInfo.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b))); r == 0 {
		return pi, false
	}
	page := le64(b[80:])
	pi.commit, pi.limit = le64(b[8:])*page, le64(b[16:])*page
	pi.total, pi.avail, pi.cache = le64(b[32:])*page, le64(b[40:])*page, le64(b[48:])*page
	pi.paged, pi.nonpaged = le64(b[64:])*page, le64(b[72:])*page
	pi.handles, pi.procs, pi.threads = le32(b[88:]), le32(b[92:]), le32(b[96:])
	return pi, true
}

func loadMemory(info *Info) {
	if pi, ok := readPerfInfo(); ok {
		info.Mem.Total = pi.total
	}
}

type coreTimes struct{ idle, total uint64 }

// perfEvery is how often the process/handle/pool counters refresh —
// GetPerformanceInfo costs a few milliseconds, and these change slowly.
const perfEvery = 5 * time.Second

func (p *platform) sample(s *Sample, info *Info, withGPU bool) {
	p.sampleCPU(s)
	// Physical memory and the commit charge are microsecond calls: read
	// them every tick. The rest comes from the cached slow query.
	var msx [64]byte // MEMORYSTATUSEX
	binary.LittleEndian.PutUint32(msx[:], uint32(len(msx)))
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&msx[0]))); r != 0 {
		s.MemTotal, s.MemAvail = le64(msx[8:]), le64(msx[16:])
		s.MemUsed = s.MemTotal - min(s.MemAvail, s.MemTotal)
		s.CommitLimit = le64(msx[24:])
		s.Commit = s.CommitLimit - min(le64(msx[32:]), s.CommitLimit)
	}
	if p.perfAt.IsZero() || time.Since(p.perfAt) >= perfEvery {
		if pi, ok := readPerfInfo(); ok {
			p.perf, p.perfAt = pi, time.Now()
		}
	}
	s.MemCached = p.perf.cache
	s.PagedPool, s.NonPagedPool = p.perf.paged, p.perf.nonpaged
	s.Handles, s.Procs, s.Threads = p.perf.handles, p.perf.procs, p.perf.threads
	ms, _, _ := procGetTickCount64.Call()
	s.Uptime = time.Duration(ms) * time.Millisecond
	p.sampleDisks(s, info)
	p.sampleNICs(s, info)
	if withGPU && len(info.GPUs) > 0 {
		p.sampleGPU(s, info)
	}
	s.Battery = sampleBattery(info.Battery)
}

// sampleCPU reads per-processor busy time. SystemProcessorPerformance-
// Information counts idle inside kernel time, so busy = kernel + user − idle.
func (p *platform) sampleCPU(s *Sample) {
	const rec = 48 // SYSTEM_PROCESSOR_PERFORMANCE_INFORMATION on x64
	buf := make([]byte, rec*max(64, runtime.NumCPU()))
	var ret uint32
	st, _, _ := procNtQuerySystemInformation.Call(8, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&ret)))
	if st != 0 {
		return
	}
	n := int(ret) / rec
	cur := make([]coreTimes, n)
	for i := range cur {
		r := buf[i*rec:]
		cur[i] = coreTimes{idle: le64(r), total: le64(r[8:]) + le64(r[16:])}
	}
	if len(p.cores) == n {
		s.Cores = make([]float64, n)
		var dt, di uint64
		for i := range cur {
			t, d := cur[i].total-p.cores[i].total, cur[i].idle-p.cores[i].idle
			if t > 0 {
				s.Cores[i] = clampPct(float64(t-min(d, t)) / float64(t) * 100)
			}
			dt, di = dt+t, di+min(d, t)
		}
		if dt > 0 {
			s.CPU = clampPct(float64(dt-di) / float64(dt) * 100)
		}
	} else {
		s.Cores = make([]float64, n)
	}
	p.cores = cur
	// CallNtPowerInformation(ProcessorInformation): 24 bytes per processor.
	pw := make([]byte, 24*n)
	if st, _, _ := procCallNtPowerInformation.Call(11, 0, 0, uintptr(unsafe.Pointer(&pw[0])), uintptr(len(pw))); st == 0 && n > 0 {
		var sum uint64
		for i := 0; i < n; i++ {
			sum += uint64(le32(pw[i*24+8:]))
		}
		s.MHz = int(sum / uint64(n))
	}
}

func clampPct(v float64) float64 { return max(0, min(100, v)) }

func batteryPresent() bool {
	var b [12]byte
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&b[0]))); r == 0 {
		return false
	}
	return b[1] != 128 && b[1] != 255 // no system battery / unknown status
}

func sampleBattery(present bool) *BatterySample {
	if !present {
		return nil
	}
	var b [12]byte
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&b[0]))); r == 0 {
		return nil
	}
	bs := &BatterySample{Percent: int(b[2]), OnAC: b[0] == 1, Charging: b[1]&8 != 0}
	if b[2] == 255 {
		bs.Percent = -1
	}
	if secs := le32(b[4:]); secs != 0xFFFFFFFF {
		bs.Remain = time.Duration(secs) * time.Second
	}
	return bs
}
