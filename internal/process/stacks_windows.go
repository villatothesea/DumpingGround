//go:build windows

package process

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	pssCaptureThreads       = 0x80
	pssCaptureThreadContext = 0x100
	pssWalkThreads          = 3
	machineAMD64            = 0x8664
	contextFull             = 0x100007
	threadGetContext        = 0x0008
	threadQueryInformation  = 0x0040
	symOptUndname           = 0x2
	symOptLoadLines         = 0x10
)

type contextX64 struct {
	P1Home, P2Home, P3Home, P4Home, P5Home, P6Home                                    uint64
	ContextFlags                                                                      uint32
	MxCsr                                                                             uint32
	SegCs, SegDs, SegEs, SegFs, SegGs, SegSs                                          uint16
	EFlags                                                                            uint32
	Dr0, Dr1, Dr2, Dr3, Dr6, Dr7                                                      uint64
	Rax, Rcx, Rdx, Rbx, Rsp, Rbp, Rsi, Rdi, R8, R9, R10, R11, R12, R13, R14, R15, Rip uint64
}
type pssThreadEntry struct {
	ExitStatus                                 uint32
	TebBaseAddress                             uintptr
	ProcessID, ThreadID                        uint32
	AffinityMask                               uintptr
	Priority, BasePriority                     int32
	LastArg                                    uintptr
	LastSyscall                                uint16
	CreateTime, ExitTime, KernelTime, UserTime syscall.Filetime
	Win32StartAddress                          uintptr
	CaptureTime                                syscall.Filetime
	Flags                                      uint32
	SuspendCount, ContextSize                  uint16
	ContextRecord                              *contextX64
}
type stackFrame64 struct {
	InstructionOffset, ReturnOffset, FrameOffset, StackOffset, BackingStoreOffset uint64
	FuncTableEntry                                                                uintptr
	Params                                                                        [4]uint64
	Reserved                                                                      [5]uint64
	Virtual                                                                       uint32
	Reserved2                                                                     uint64
}
type symbolInfo struct {
	SizeOfStruct, TypeIndex                   uint32
	Reserved                                  [2]uint64
	Index, Size                               uint32
	ModBase                                   uint64
	Flags                                     uint32
	Value, Address                            uint64
	Register, Scope, Tag, NameLen, MaxNameLen uint32
	Name                                      [256]byte
}

var (
	pssCapture        = kernel32.NewProc("PssCaptureSnapshot")
	pssWalk           = kernel32.NewProc("PssWalkSnapshot")
	pssMarkerCreate   = kernel32.NewProc("PssWalkMarkerCreate")
	pssMarkerFree     = kernel32.NewProc("PssWalkMarkerFree")
	pssFree           = kernel32.NewProc("PssFreeSnapshot")
	dbghelp           = syscall.NewLazyDLL("dbghelp.dll")
	stackWalk         = dbghelp.NewProc("StackWalk64")
	symInitialize     = dbghelp.NewProc("SymInitializeW")
	symCleanup        = dbghelp.NewProc("SymCleanup")
	symOptions        = dbghelp.NewProc("SymSetOptions")
	symFromAddr       = dbghelp.NewProc("SymFromAddr")
	symFunctionTable  = dbghelp.NewProc("SymFunctionTableAccess64")
	symModuleBase     = dbghelp.NewProc("SymGetModuleBase64")
	readProcessMemory = kernel32.NewProc("ReadProcessMemory")
	procOpenThread    = kernel32.NewProc("OpenThread")
	readCallback      = syscall.NewCallback(func(process, addr, buffer, size uintptr, read *uintptr) uintptr {
		var got uintptr
		ok, _, _ := readProcessMemory.Call(process, addr, buffer, size, uintptr(unsafe.Pointer(&got)))
		if read != nil {
			*read = got
		}
		return ok
	})
	functionCallback = syscall.NewCallback(func(process, addr uintptr) uintptr { v, _, _ := symFunctionTable.Call(process, addr); return v })
	moduleCallback   = syscall.NewCallback(func(process, addr uintptr) uintptr { v, _, _ := symModuleBase.Call(process, addr); return v })
)

func (c *windowsCollector) Stack(pid, threadID uint32, maxFrames int) ([]StackFrame, error) {
	if maxFrames <= 0 || maxFrames > 256 {
		maxFrames = 64
	}
	h, err := openProc(pid, processQueryInformation|processQueryLimitedInformation|processVMRead)
	if h == 0 {
		return nil, err
	}
	defer procClose.Call(h)
	th, _, _ := procOpenThread.Call(threadGetContext|threadQueryInformation, 0, uintptr(threadID))
	if th == 0 {
		return nil, syscall.GetLastError()
	}
	defer procClose.Call(th)
	var snap uintptr
	flags := uintptr(pssCaptureThreads | pssCaptureThreadContext)
	status, _, _ := pssCapture.Call(h, flags, uintptr(contextFull), uintptr(unsafe.Pointer(&snap)))
	if status != 0 || snap == 0 {
		return nil, fmt.Errorf("PssCaptureSnapshot: 0x%x", status)
	}
	defer pssFree.Call(h, snap)
	var marker uintptr
	status, _, _ = pssMarkerCreate.Call(0, uintptr(unsafe.Pointer(&marker)))
	if status != 0 {
		return nil, fmt.Errorf("PssWalkMarkerCreate: 0x%x", status)
	}
	defer pssMarkerFree.Call(marker)
	var entry pssThreadEntry
	found := false
	for {
		status, _, _ := pssWalk.Call(snap, pssWalkThreads, marker, uintptr(unsafe.Pointer(&entry)), unsafe.Sizeof(entry))
		if status != 0 {
			break
		}
		if entry.ThreadID == threadID {
			found = true
			break
		}
	}
	if !found || entry.ContextRecord == nil {
		return nil, errors.New("thread context unavailable")
	}
	ctx := *entry.ContextRecord
	ctx.ContextFlags = contextFull
	symOptions.Call(symOptUndname | symOptLoadLines)
	symInitialize.Call(h, 0, 1)
	defer symCleanup.Call(h)
	frame := stackFrame64{InstructionOffset: ctx.Rip, FrameOffset: ctx.Rbp, StackOffset: ctx.Rsp}
	out := make([]StackFrame, 0, maxFrames)
	for len(out) < maxFrames && frame.InstructionOffset != 0 {
		address := frame.InstructionOffset
		name := ""
		var displacement uint64
		si := symbolInfo{SizeOfStruct: uint32(unsafe.Sizeof(symbolInfo{})), MaxNameLen: 255}
		if ok, _, _ := symFromAddr.Call(h, uintptr(address), uintptr(unsafe.Pointer(&displacement)), uintptr(unsafe.Pointer(&si))); ok != 0 {
			name = string(si.Name[:si.NameLen])
		}
		out = append(out, StackFrame{ThreadID: threadID, Address: address, Symbol: name, Offset: displacement})
		ok, _, _ := stackWalk.Call(machineAMD64, h, th, uintptr(unsafe.Pointer(&frame)), uintptr(unsafe.Pointer(&ctx)), readCallback, functionCallback, moduleCallback, 0)
		if ok == 0 || frame.InstructionOffset == address {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty call stack")
	}
	return out, nil
}
