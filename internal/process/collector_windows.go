//go:build windows

package process

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	th32csSnapProcess              = 0x00000002
	th32csSnapThread               = 0x00000004
	th32csSnapModule               = 0x00000008
	th32csSnapModule32             = 0x00000010
	processTerminate               = 0x0001
	processQueryLimitedInformation = 0x1000
	processVMRead                  = 0x0010
	maxPath                        = 32768
)

type processEntry32 struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	BasePriority    int32
	Flags           uint32
	ExeFile         [260]uint16
}

type memoryCounters struct {
	Size                       uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type fileTime struct{ Low, High uint32 }

type moduleEntry32 struct {
	Size         uint32
	ModuleID     uint32
	ProcessID    uint32
	GlobalUsage  uint32
	ProcessUsage uint32
	BaseAddress  uintptr
	BaseSize     uint32
	Module       uintptr
	Name         [256]uint16
	Path         [260]uint16
}

type threadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePriority   int32
	DeltaPriority  int32
	Flags          uint32
}

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	psapi              = syscall.NewLazyDLL("psapi.dll")
	procCreateSnapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procFirst          = kernel32.NewProc("Process32FirstW")
	procNext           = kernel32.NewProc("Process32NextW")
	procModuleFirst    = kernel32.NewProc("Module32FirstW")
	procModuleNext     = kernel32.NewProc("Module32NextW")
	procThreadFirst    = kernel32.NewProc("Thread32First")
	procThreadNext     = kernel32.NewProc("Thread32Next")
	procOpen           = kernel32.NewProc("OpenProcess")
	procClose          = kernel32.NewProc("CloseHandle")
	procQueryPath      = kernel32.NewProc("QueryFullProcessImageNameW")
	procTimes          = kernel32.NewProc("GetProcessTimes")
	procTerminate      = kernel32.NewProc("TerminateProcess")
	procMemory         = psapi.NewProc("GetProcessMemoryInfo")
	procIO             = kernel32.NewProc("GetProcessIoCounters")
)

type sample struct {
	cpu         uint64
	read, write uint64
	at          time.Time
}
type windowsCollector struct {
	mu       sync.Mutex
	previous map[uint32]sample
}

func newCollector() Collector { return &windowsCollector{previous: make(map[uint32]sample)} }

func (c *windowsCollector) Snapshot() ([]Info, error) {
	snapshot, _, err := procCreateSnapshot.Call(th32csSnapProcess, 0)
	if snapshot == 0 || snapshot == uintptr(syscall.InvalidHandle) {
		if err == syscall.Errno(0) {
			err = syscall.GetLastError()
		}
		return nil, err
	}
	defer procClose.Call(snapshot)
	entry := processEntry32{Size: uint32(unsafe.Sizeof(processEntry32{}))}
	ret, _, firstErr := procFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		if firstErr == syscall.Errno(0) {
			firstErr = syscall.GetLastError()
		}
		return nil, firstErr
	}
	var result []Info
	for {
		info := c.readEntry(&entry)
		result = append(result, info)
		ret, _, _ = procNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}
	seen := make(map[uint32]struct{}, len(result))
	for _, info := range result {
		seen[info.PID] = struct{}{}
	}
	c.mu.Lock()
	for pid := range c.previous {
		if _, ok := seen[pid]; !ok {
			delete(c.previous, pid)
		}
	}
	c.mu.Unlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (c *windowsCollector) readEntry(entry *processEntry32) Info {
	name := syscall.UTF16ToString(entry.ExeFile[:])
	info := Info{PID: entry.ProcessID, PPID: entry.ParentProcessID, Name: name, Threads: entry.Threads}
	if entry.ProcessID == 0 {
		return info
	}
	h, _, _ := procOpen.Call(processQueryLimitedInformation|processVMRead, 0, uintptr(entry.ProcessID))
	if h == 0 {
		h, _, _ = procOpen.Call(processQueryLimitedInformation, 0, uintptr(entry.ProcessID))
	}
	if h == 0 {
		return info
	}
	defer procClose.Call(h)
	info.Path = queryPath(h)
	var memory memoryCounters
	memory.Size = uint32(unsafe.Sizeof(memory))
	if ret, _, _ := procMemory.Call(h, uintptr(unsafe.Pointer(&memory)), uintptr(unsafe.Sizeof(memory))); ret != 0 {
		info.Memory = uint64(memory.WorkingSetSize)
	}
	var created, exited, kernel, user fileTime
	if ret, _, _ := procTimes.Call(h, uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); ret != 0 {
		cpu := fileTimeValue(kernel) + fileTimeValue(user)
		var io ioCounters
		if ret, _, _ := procIO.Call(h, uintptr(unsafe.Pointer(&io))); ret != 0 {
			info.ReadBytes, info.WriteBytes = io.ReadTransferCount, io.WriteTransferCount
		}
		c.mu.Lock()
		prev, ok := c.previous[info.PID]
		now := time.Now()
		if ok && now.After(prev.at) {
			elapsed := now.Sub(prev.at).Seconds()
			if elapsed > 0 {
				info.CPU = float64(cpu-prev.cpu) / 10_000_000 / elapsed / float64(maxInt(runtime.NumCPU(), 1)) * 100
				if info.CPU < 0 {
					info.CPU = 0
				}
				if info.CPU > 100 {
					info.CPU = 100
				}
			}
		}
		c.previous[info.PID] = sample{cpu: cpu, read: info.ReadBytes, write: info.WriteBytes, at: now}
		c.mu.Unlock()
	}
	return info
}

func queryPath(h uintptr) string {
	for size := 512; size <= maxPath; size *= 2 {
		buf := make([]uint16, size)
		length := uint32(len(buf))
		ret, _, _ := procQueryPath.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&length)))
		if ret != 0 {
			return syscall.UTF16ToString(buf[:length])
		}
		if syscall.GetLastError() != syscall.Errno(122) {
			return ""
		}
	}
	return ""
}

func fileTimeValue(v fileTime) uint64 { return uint64(v.High)<<32 | uint64(v.Low) }
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (c *windowsCollector) Terminate(pid uint32, tree bool) error {
	ids := []uint32{pid}
	if tree {
		children, err := c.children()
		if err != nil {
			return err
		}
		ids = append(ids, descendants(pid, children)...)
	}
	var failures []error
	for i := len(ids) - 1; i >= 0; i-- {
		h, _, err := procOpen.Call(processTerminate, 0, uintptr(ids[i]))
		if h == 0 {
			failures = append(failures, fmt.Errorf("PID %d: %w", ids[i], err))
			continue
		}
		ret, _, termErr := procTerminate.Call(h, 1)
		procClose.Call(h)
		if ret == 0 {
			failures = append(failures, fmt.Errorf("PID %d: %w", ids[i], termErr))
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

func (c *windowsCollector) children() (map[uint32][]uint32, error) {
	snapshot, _, err := procCreateSnapshot.Call(th32csSnapProcess, 0)
	if snapshot == 0 || snapshot == uintptr(syscall.InvalidHandle) {
		return nil, err
	}
	defer procClose.Call(snapshot)
	entry := processEntry32{Size: uint32(unsafe.Sizeof(processEntry32{}))}
	out := make(map[uint32][]uint32)
	ret, _, _ := procFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ret != 0 {
		out[entry.ParentProcessID] = append(out[entry.ParentProcessID], entry.ProcessID)
		ret, _, _ = procNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return out, nil
}

func descendants(root uint32, children map[uint32][]uint32) []uint32 {
	var out []uint32
	var visit func(uint32)
	visit = func(id uint32) {
		for _, child := range children[id] {
			out = append(out, child)
			visit(child)
		}
	}
	visit(root)
	return out
}
