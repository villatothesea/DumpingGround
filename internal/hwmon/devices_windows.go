//go:build windows

package hwmon

import (
	"slices"
	"strings"
	"syscall"
	"unsafe"
)

const (
	spdrpDeviceDesc = 0x00
	spdrpMfg        = 0x0B
	spdrpFriendly   = 0x0C
)

// spDevInfoData mirrors SP_DEVINFO_DATA so its x64 size comes from the
// compiler.
type spDevInfoData struct {
	cbSize    uint32
	ClassGuid syscall.GUID
	DevInst   uint32
	Reserved  uintptr
}

func guidOf(d1 uint32, d2, d3 uint16, d4 [8]byte) syscall.GUID {
	return syscall.GUID{Data1: d1, Data2: d2, Data3: d3, Data4: d4}
}

var classStd = [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18}

// deviceClasses are the Plug-and-Play classes worth naming on the Computer
// page. Enumerating just these is far cheaper than DIGCF_ALLCLASSES and
// leaves out the anonymous system resources; monitors are collected
// separately so their EDID can be filtered to the ones actually attached.
var deviceClasses = []struct {
	g    syscall.GUID
	name string
}{
	{guidOf(0x4d36e972, 0xe325, 0x11ce, classStd), "Network"},
	{guidOf(0x4d36e96c, 0xe325, 0x11ce, classStd), "Audio"},
	{guidOf(0x36fc9e60, 0xc465, 0x11cf, [8]byte{0x80, 0x56, 0x44, 0x45, 0x53, 0x54, 0x00, 0x00}), "USB"},
	{guidOf(0x745a17a0, 0x74d3, 0x11d0, [8]byte{0xb6, 0xfe, 0x00, 0xa0, 0xc9, 0x0f, 0x57, 0xda}), "Input device"},
	{guidOf(0xe0cbf06c, 0xcd8b, 0x4647, [8]byte{0xbb, 0x8a, 0x26, 0x3b, 0x43, 0xf0, 0xf9, 0x74}), "Bluetooth"},
	{guidOf(0x6bdd1fc6, 0x810f, 0x11d0, [8]byte{0xbe, 0xc7, 0x08, 0x00, 0x2b, 0xe2, 0x09, 0x2f}), "Camera"},
	{guidOf(0x4d36e97b, 0xe325, 0x11ce, classStd), "Storage controller"},
	{guidOf(0x4d36e96a, 0xe325, 0x11ce, classStd), "Storage controller"},
	{guidOf(0x4d36e968, 0xe325, 0x11ce, classStd), "Display"},
	{guidOf(0x4d36e96e, 0xe325, 0x11ce, classStd), "Monitor"},
	{guidOf(0x4d36e965, 0xe325, 0x11ce, classStd), "Optical drive"},
	{guidOf(0x4d36e967, 0xe325, 0x11ce, classStd), "Disk"},
	{guidOf(0x4d36e96b, 0xe325, 0x11ce, classStd), "Keyboard"},
	{guidOf(0x4d36e96f, 0xe325, 0x11ce, classStd), "Mouse"},
	{guidOf(0x71a27cdd, 0x812a, 0x11d0, [8]byte{0xbe, 0xc7, 0x08, 0x00, 0x2b, 0xe2, 0x09, 0x2f}), "Volume"},
	{guidOf(0xf2e7dd72, 0x6468, 0x4e36, [8]byte{0xb6, 0xf1, 0x64, 0x88, 0xf4, 0x2c, 0x1b, 0x52}), "Firmware"},
	{guidOf(0x50127dc3, 0x0f36, 0x415e, [8]byte{0xa6, 0xcc, 0x4c, 0xb3, 0xbe, 0x91, 0x0b, 0x65}), "Processor"},
}

// loadDevices enumerates present Plug-and-Play devices through SetupAPI —
// no driver, no WMI. It returns the device instance ids of the monitors it
// found, which the display reader uses to ignore stale EDID entries.
func loadDevices(info *Info) map[string]bool {
	monitors := map[string]bool{}
	seen := map[string]bool{}
	sz := uint32(unsafe.Sizeof(spDevInfoData{}))
	for _, dc := range deviceClasses {
		h, _, _ := procSetupDiGetClassDevs.Call(uintptr(unsafe.Pointer(&dc.g)), 0, 0, digcfPresent)
		if h == invalidHandleValue || h == 0 {
			continue
		}
		for i := 0; ; i++ {
			di := spDevInfoData{cbSize: sz}
			if r, _, _ := procSetupDiEnumDeviceInfo.Call(h, uintptr(i), uintptr(unsafe.Pointer(&di))); r == 0 {
				break
			}
			name := devProp(h, unsafe.Pointer(&di), spdrpFriendly)
			if name == "" {
				name = devProp(h, unsafe.Pointer(&di), spdrpDeviceDesc)
			}
			if name == "" {
				continue
			}
			if dc.name == "Monitor" {
				if id := deviceInstanceID(h, unsafe.Pointer(&di)); id != "" {
					monitors[strings.ToLower(id)] = true
				}
				continue // monitors are shown from their EDID instead
			}
			// Endpoint-heavy classes repeat a device name; keep one row.
			key := dc.name + "\x00" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			vendor := devProp(h, unsafe.Pointer(&di), spdrpMfg)
			info.Devices = append(info.Devices, DeviceInfo{Name: name, Class: dc.name, Vendor: vendor})
			if dc.name == "Audio" {
				info.Audio = append(info.Audio, AudioInfo{Name: name, Vendor: vendor})
			}
		}
		procSetupDiDestroyDeviceInfoList.Call(h)
	}
	slices.SortFunc(info.Devices, func(a, b DeviceInfo) int {
		if c := strings.Compare(a.Class, b.Class); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	slices.SortFunc(info.Audio, func(a, b AudioInfo) int { return strings.Compare(a.Name, b.Name) })
	return monitors
}

// devProp reads one SetupAPI device property as a string.
func devProp(h uintptr, di unsafe.Pointer, prop uint32) string {
	var needed uint32
	procSetupDiGetDeviceRegistryProp.Call(h, uintptr(di), uintptr(prop), 0, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 || needed > 8192 {
		return ""
	}
	buf := make([]uint16, needed/2+1)
	if r, _, _ := procSetupDiGetDeviceRegistryProp.Call(h, uintptr(di), uintptr(prop), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&needed))); r == 0 {
		return ""
	}
	return strings.Join(strings.Fields(syscall.UTF16ToString(buf)), " ")
}

func deviceInstanceID(h uintptr, di unsafe.Pointer) string {
	var needed uint32
	procSetupDiGetDeviceInstanceID.Call(h, uintptr(di), 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 || needed > 1024 {
		return ""
	}
	buf := make([]uint16, needed)
	if r, _, _ := procSetupDiGetDeviceInstanceID.Call(h, uintptr(di), uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), 0); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
