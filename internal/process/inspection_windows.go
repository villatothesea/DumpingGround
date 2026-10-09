//go:build windows

package process

import (
	"syscall"
	"unsafe"
)

func (c *windowsCollector) Modules(pid uint32) ([]Module, error) {
	snapshot, _, err := procCreateSnapshot.Call(th32csSnapModule|th32csSnapModule32, uintptr(pid))
	if snapshot == 0 || snapshot == uintptr(syscall.InvalidHandle) {
		return nil, err
	}
	defer procClose.Call(snapshot)
	entry := moduleEntry32{Size: uint32(unsafe.Sizeof(moduleEntry32{}))}
	ret, _, firstErr := procModuleFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		if firstErr == syscall.Errno(0) {
			firstErr = syscall.GetLastError()
		}
		return nil, firstErr
	}
	var modules []Module
	for ret != 0 {
		modules = append(modules, Module{Name: syscall.UTF16ToString(entry.Name[:]), Path: syscall.UTF16ToString(entry.Path[:]), Size: uint64(entry.BaseSize)})
		ret, _, _ = procModuleNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return modules, nil
}

func (c *windowsCollector) Threads(pid uint32) ([]Thread, error) {
	snapshot, _, err := procCreateSnapshot.Call(th32csSnapThread, 0)
	if snapshot == 0 || snapshot == uintptr(syscall.InvalidHandle) {
		return nil, err
	}
	defer procClose.Call(snapshot)
	entry := threadEntry32{Size: uint32(unsafe.Sizeof(threadEntry32{}))}
	ret, _, firstErr := procThreadFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		if firstErr == syscall.Errno(0) {
			firstErr = syscall.GetLastError()
		}
		return nil, firstErr
	}
	var threads []Thread
	for ret != 0 {
		if entry.OwnerProcessID == pid {
			threads = append(threads, Thread{ID: entry.ThreadID, OwnerPID: entry.OwnerProcessID, Priority: entry.BasePriority})
		}
		ret, _, _ = procThreadNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return threads, nil
}
