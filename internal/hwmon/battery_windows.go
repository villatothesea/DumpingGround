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
	ioctlBatteryQueryTag    = 0x294040 // IOCTL_BATTERY_QUERY_TAG
	ioctlBatteryQueryInfo   = 0x294044 // IOCTL_BATTERY_QUERY_INFORMATION
	ioctlBatteryQueryStatus = 0x29404C // IOCTL_BATTERY_QUERY_STATUS

	batteryUnknownRate     = -0x80000000 // BATTERY_UNKNOWN_RATE
	batteryUnknownCapacity = 0xFFFFFFFF  // BATTERY_UNKNOWN_CAPACITY

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
func readBatteryInfo() (*BatteryInfo, string) {
	// There can be several battery devices (some laptops expose a bridge
	// battery as well); the first one that answers is the main pack.
	hdevinfo, _, _ := procSetupDiGetClassDevs.Call(uintptr(unsafe.Pointer(&guidDeviceBattery)), 0, 0, digcfPresent|digcfDeviceInterface)
	if hdevinfo == invalidHandleValue || hdevinfo == 0 {
		return nil, ""
	}
	defer procSetupDiDestroyDeviceInfoList.Call(hdevinfo)

	didSize := uint32(unsafe.Sizeof(spDeviceInterfaceData{}))
	pathOff := unsafe.Offsetof(spDeviceInterfaceDetailData{}.DevicePath)
	detailSize := uint32(unsafe.Sizeof(spDeviceInterfaceDetailData{}))

	for i := 0; ; i++ {
		did := spDeviceInterfaceData{cbSize: didSize}
		r, _, _ := procSetupDiEnumDeviceInterfaces.Call(hdevinfo, 0, uintptr(unsafe.Pointer(&guidDeviceBattery)), uintptr(i), uintptr(unsafe.Pointer(&did)))
		if r == 0 {
			return nil, ""
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
		path := utf16Bytes(detail[pathOff:])
		if b := readBatteryPath(path); b != nil {
			return b, path
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

// readBatteryStatus asks the driver for BATTERY_STATUS — the live rate
// (mW, negative while discharging) and remaining capacity (mWh). This is
// the firmware's own power telemetry; rate 0 / capacity 0 mean "the
// driver didn't say", not zero draw.
func readBatteryStatus(path string) (rateMW, capMWH int) {
	if path == "" {
		return 0, 0
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, 0
	}
	defer syscall.CloseHandle(h)

	tagOut := make([]byte, 4)
	if _, err := ioctl(h, ioctlBatteryQueryTag, make([]byte, 4), tagOut); err != nil {
		return 0, 0
	}
	// BATTERY_WAIT_STATUS_DATA: tag, timeout 0 (don't wait), power-state
	// and capacity thresholds ignored when the timeout is 0.
	wait := make([]byte, 20)
	copy(wait, tagOut)
	out := make([]byte, 16) // BATTERY_STATUS: PowerState, Capacity, Voltage, Rate
	if _, err := ioctl(h, ioctlBatteryQueryStatus, wait, out); err != nil {
		return 0, 0
	}
	if cap32 := le32(out[4:]); cap32 != batteryUnknownCapacity {
		capMWH = int(cap32)
	}
	if rate := int32(le32(out[12:])); rate != batteryUnknownRate {
		rateMW = int(rate)
	}
	return rateMW, capMWH
}
