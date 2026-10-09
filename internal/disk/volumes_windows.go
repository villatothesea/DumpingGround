//go:build windows

package disk

import (
	"syscall"
	"unsafe"
)

var (
	volumeKernel32       = syscall.NewLazyDLL("kernel32.dll")
	getLogicalDrivesProc = volumeKernel32.NewProc("GetLogicalDrives")
	getDriveTypeProc     = volumeKernel32.NewProc("GetDriveTypeW")
	getDiskFreeSpaceProc = volumeKernel32.NewProc("GetDiskFreeSpaceExW")
	getVolumeInfoProc    = volumeKernel32.NewProc("GetVolumeInformationW")
)

func Volumes() []Volume {
	mask, _, _ := getLogicalDrivesProc.Call()
	var volumes []Volume
	for i := uint32(0); i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		path := string(rune('A'+i)) + ":\\"
		ptr, _ := syscall.UTF16PtrFromString(path)
		typeValue, _, _ := getDriveTypeProc.Call(uintptr(unsafe.Pointer(ptr)))
		if typeValue == 1 {
			continue
		}
		var free, total, totalFree uint64
		if ret, _, _ := getDiskFreeSpaceProc.Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree))); ret == 0 {
			continue
		}
		label := make([]uint16, 260)
		filesystem := make([]uint16, 64)
		var serial, maxComponent, flags uint32
		getVolumeInfoProc.Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&label[0])), uintptr(len(label)), uintptr(unsafe.Pointer(&serial)), uintptr(unsafe.Pointer(&maxComponent)), uintptr(unsafe.Pointer(&flags)), uintptr(unsafe.Pointer(&filesystem[0])), uintptr(len(filesystem)))
		volumes = append(volumes, Volume{Path: path, Label: syscall.UTF16ToString(label), Total: total, Free: free})
	}
	return volumes
}
