//go:build windows

package hwmon

import (
	"syscall"
	"unsafe"
)

// GUID_DEVINTERFACE_DISK, and the DEVPKEY namespace that holds a PCI
// function's negotiated link registers.
var (
	guidDevInterfaceDisk = guidOf(0x53f56307, 0xb6bf, 0x11d0, [8]byte{0x94, 0xf2, 0x00, 0xa0, 0xc9, 0x1e, 0xfb, 0x8b})
	guidPciDevice        = guidOf(0x3ab22e31, 0x8264, 0x4b4e, [8]byte{0x9a, 0xf5, 0xa8, 0xd2, 0xd8, 0xe3, 0x3e, 0x62})
)

type devPropKey struct {
	g   syscall.GUID
	pid uint32
}

// DEVPKEY_PciDevice_CurrentLinkSpeed/Width and the Max pair. The values
// are the LNKSTS register fields: 1 = Gen1, 2 = Gen2, ... and a lane
// count.
var (
	propPciLinkSpeed    = devPropKey{guidPciDevice, 8}
	propPciLinkWidth    = devPropKey{guidPciDevice, 9}
	propPciLinkSpeedMax = devPropKey{guidPciDevice, 0x0A}
	propPciLinkWidthMax = devPropKey{guidPciDevice, 0x0B}
)

const fileDeviceDisk = 7 // FILE_DEVICE_DISK in STORAGE_DEVICE_NUMBER

// diskDevInsts maps physical drive numbers to their device nodes: every
// disk interface path is opened once and asked which device number it
// answers to, which is the number in \\.\PhysicalDriveN.
func diskDevInsts() map[uint32]uint32 {
	out := map[uint32]uint32{}
	set, _, _ := procSetupDiGetClassDevs.Call(uintptr(unsafe.Pointer(&guidDevInterfaceDisk)), 0, 0, digcfPresent|digcfDeviceInterface)
	if set == invalidHandleValue || set == 0 {
		return out
	}
	defer procSetupDiDestroyDeviceInfoList.Call(set)
	var ifd [32]byte // SP_DEVICE_INTERFACE_DATA: cbSize, GUID, flags, reserved
	put32(ifd[:], uint32(len(ifd)))
	var di spDevInfoData
	di.cbSize = uint32(unsafe.Sizeof(di))
	for i := 0; ; i++ {
		if r, _, _ := procSetupDiEnumDeviceInterfaces.Call(set, 0, uintptr(unsafe.Pointer(&guidDevInterfaceDisk)), uintptr(i), uintptr(unsafe.Pointer(&ifd[0]))); r == 0 {
			break
		}
		var need uint32
		procSetupDiGetDeviceInterfaceDet.Call(set, uintptr(unsafe.Pointer(&ifd[0])), 0, 0, uintptr(unsafe.Pointer(&need)), 0)
		if need < 10 || need > 4096 {
			continue
		}
		buf := make([]byte, need)
		put32(buf, 8) // SP_DEVICE_INTERFACE_DETAIL_DATA_W.cbSize on 64-bit
		if r, _, _ := procSetupDiGetDeviceInterfaceDet.Call(set, uintptr(unsafe.Pointer(&ifd[0])), uintptr(unsafe.Pointer(&buf[0])), uintptr(need), 0, uintptr(unsafe.Pointer(&di))); r == 0 {
			continue
		}
		// cbSize claims 8 bytes for alignment, but DevicePath begins
		// right after the DWORD at offset 4.
		h, err := openNoAccess(utf16Bytes(buf[4:]))
		if err != nil {
			continue
		}
		var num [12]byte // STORAGE_DEVICE_NUMBER
		if _, err := ioctl(h, ioctlStorageDeviceNum, nil, num[:]); err == nil && le32(num[0:]) == fileDeviceDisk {
			out[le32(num[4:])] = di.DevInst
		}
		syscall.CloseHandle(h)
	}
	return out
}

// devNodeProp reads a uint32 DEVPKEY off one device node.
func devNodeProp(inst uint32, key *devPropKey) (uint32, bool) {
	var typ, size uint32
	var b [4]byte
	size = 4
	r, _, _ := procCMGetDevNodeProp.Call(uintptr(inst), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), 0)
	if r != 0 || size < 4 {
		return 0, false
	}
	return le32(b[:]), true
}

// pcieLink reads the negotiated PCI link the drive hangs off. The disk's
// own devnode has no link registers — they belong to the PCI function
// above it (the NVMe or SATA controller) — so ancestors are walked until
// one answers.
func pcieLink(inst uint32) (gen, lanes, maxGen, maxLanes int) {
	for depth := 0; depth < 8 && inst != 0; depth++ {
		if v, ok := devNodeProp(inst, &propPciLinkSpeed); ok && v > 0 {
			gen = int(v)
			lanes = u2i(devNodeProp(inst, &propPciLinkWidth))
			maxGen = u2i(devNodeProp(inst, &propPciLinkSpeedMax))
			maxLanes = u2i(devNodeProp(inst, &propPciLinkWidthMax))
			return gen, lanes, maxGen, maxLanes
		}
		var parent uint32
		if r, _, _ := procCMGetParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(inst), 0); r != 0 {
			break
		}
		inst = parent
	}
	return 0, 0, 0, 0
}

func u2i(v uint32, ok bool) int {
	if !ok {
		return 0
	}
	return int(v)
}

func put32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}
