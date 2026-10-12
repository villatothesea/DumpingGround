//go:build windows

package hwmon

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"unsafe"
)

const (
	// Battery class driver IOCTLs (FILE_DEVICE_BATTERY, FILE_READ_ACCESS).
	ioctlBatteryQueryTag  = 0x294040 // IOCTL_BATTERY_QUERY_TAG
	ioctlBatteryQueryInfo = 0x294044 // IOCTL_BATTERY_QUERY_INFORMATION

	digcfPresent         = 0x02
	digcfDeviceInterface = 0x10

	invalidHandleValue = ^uintptr(0)
)

var guidDeviceBattery = syscall.GUID{Data1: 0x72631e54, Data2: 0x78a4, Data3: 0x11d0, Data4: [8]byte{0xbc, 0xf7, 0x00, 0xaa, 0x00, 0xb7, 0xb3, 0x2a}}

// These mirror the SetupAPI structs so their x64 sizes and field offsets
// come from the compiler instead of from memory (SP_DEVICE_INTERFACE_DATA
// hides an ULONG_PTR Reserved field that changes both).
type spDeviceInterfaceData struct {
	cbSize             uint32
	InterfaceClassGuid syscall.GUID
	Flags              uint32
	Reserved           uintptr
}

type spDeviceInterfaceDetailData struct {
	cbSize     uint32
	DevicePath uint16
}

// readBatteryInfo asks the battery driver for BATTERY_INFORMATION — design
// capacity, current full-charge capacity and cycle count. That is how
// battery wear is measured: the fraction of the original capacity the cell
// still holds, from the firmware rather than inferred from the charge
// percentage.
func readBatteryInfo() *BatteryInfo {
	// There can be several battery devices (some laptops expose a bridge
	// battery as well); the first one that answers is the main pack.
	hdevinfo, _, _ := procSetupDiGetClassDevs.Call(uintptr(unsafe.Pointer(&guidDeviceBattery)), 0, 0, digcfPresent|digcfDeviceInterface)
	if hdevinfo == invalidHandleValue || hdevinfo == 0 {
		return nil
	}
	defer procSetupDiDestroyDeviceInfoList.Call(hdevinfo)

	didSize := uint32(unsafe.Sizeof(spDeviceInterfaceData{}))
	pathOff := unsafe.Offsetof(spDeviceInterfaceDetailData{}.DevicePath)
	detailSize := uint32(unsafe.Sizeof(spDeviceInterfaceDetailData{}))

	for i := 0; ; i++ {
		did := spDeviceInterfaceData{cbSize: didSize}
		r, _, _ := procSetupDiEnumDeviceInterfaces.Call(hdevinfo, 0, uintptr(unsafe.Pointer(&guidDeviceBattery)), uintptr(i), uintptr(unsafe.Pointer(&did)))
		if r == 0 {
			return nil
		}
		var needed uint32
		procSetupDiGetDeviceInterfaceDet.Call(hdevinfo, uintptr(unsafe.Pointer(&did)), 0, 0, uintptr(unsafe.Pointer(&needed)), 0)
		if needed == 0 || needed > 4096 {
			continue
		}
		detail := make([]byte, needed)
		binary.LittleEndian.PutUint32(detail, detailSize)
		if r, _, _ := procSetupDiGetDeviceInterfaceDet.Call(hdevinfo, uintptr(unsafe.Pointer(&did)), uintptr(unsafe.Pointer(&detail[0])), uintptr(needed), 0, 0); r == 0 {
			continue
		}
		if b := readBatteryPath(utf16Bytes(detail[pathOff:])); b != nil {
			return b
		}
	}
}

func readBatteryPath(path string) *BatteryInfo {
	if path == "" {
		return nil
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(h)

	// Ask for a tag first; it identifies the battery for the information
	// query (and changes when the pack is swapped).
	tagOut := make([]byte, 4)
	if _, err := ioctl(h, ioctlBatteryQueryTag, make([]byte, 4), tagOut); err != nil {
		return nil
	}
	q := make([]byte, 12) // BATTERY_QUERY_INFORMATION: tag, level, AtRate
	binary.LittleEndian.PutUint32(q, le32(tagOut))
	binary.LittleEndian.PutUint32(q[4:], 0) // BatteryInformation
	out := make([]byte, 36)                 // BATTERY_INFORMATION
	if _, err := ioctl(h, ioctlBatteryQueryInfo, q, out); err != nil {
		return nil
	}
	design, full := uint64(le32(out[12:])), uint64(le32(out[16:]))
	if design == 0xFFFFFFFF {
		design = 0
	}
	if full == 0xFFFFFFFF {
		full = 0
	}
	return &BatteryInfo{
		DesignCapacity: design,
		FullCapacity:   full,
		CycleCount:     uint64(le32(out[32:])),
		Chemistry:      string(bytes.TrimRight(out[8:12], "\x00")),
	}
}
