//go:build windows

package hwmon

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	ioctlDiskPerformance   = 0x70020 // IOCTL_DISK_PERFORMANCE
	ioctlDiskGeometryEx    = 0x700A0 // IOCTL_DISK_GET_DRIVE_GEOMETRY_EX
	ioctlStorageQueryProp  = 0x2D1400
	ioctlStorageDeviceNum  = 0x2D1080 // IOCTL_STORAGE_GET_DEVICE_NUMBER
	storageDeviceProperty  = 0
	storageSeekPenaltyProp = 7
)

// diskHandle keeps one physical drive open (no access rights — the
// storage and performance IOCTLs are FILE_ANY_ACCESS) with the previous
// counters for rate math.
type diskHandle struct {
	h    syscall.Handle
	prev diskPerf
	ok   bool
}

type diskPerf struct {
	bytesRead, bytesWritten           uint64
	readTime, writeTime, idleTime     uint64
	readCount, writeCount, queueDepth uint32
	queryTime                         uint64
}

func ioctl(h syscall.Handle, code uint32, in, out []byte) (uint32, error) {
	var inp, outp *byte
	if len(in) > 0 {
		inp = &in[0]
	}
	if len(out) > 0 {
		outp = &out[0]
	}
	var ret uint32
	err := syscall.DeviceIoControl(h, code, inp, uint32(len(in)), outp, uint32(len(out)), &ret, nil)
	return ret, err
}

func openNoAccess(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.CreateFile(p, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
}

func (p *platform) loadDisks(info *Info) {
	byNumber := map[uint32]int{} // physical drive number → index in info.Disks
	for i := 0; i < 32; i++ {
		h, err := openNoAccess(fmt.Sprintf(`\\.\PhysicalDrive%d`, i))
		if err != nil {
			continue
		}
		d := DiskInfo{Index: i}
		var geo [64]byte
		if n, err := ioctl(h, ioctlDiskGeometryEx, nil, geo[:]); err == nil && n >= 32 {
			d.Size = le64(geo[24:]) // DISK_GEOMETRY (24 bytes) then DiskSize
		}
		if d.Size == 0 {
			syscall.CloseHandle(h) // empty card reader
			continue
		}
		d.Model, d.Bus = storageDescriptor(h)
		d.Kind = diskKind(h, d.Bus)
		byNumber[uint32(i)] = len(info.Disks)
		info.Disks = append(info.Disks, d)
		p.disks = append(p.disks, &diskHandle{h: h})
	}
	for l := 'A'; l <= 'Z'; l++ {
		root := string(l) + `:\`
		rp, _ := syscall.UTF16PtrFromString(root)
		if t, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(rp))); t != 3 && t != 2 {
			continue
		}
		h, err := openNoAccess(`\\.\` + string(l) + `:`)
		if err != nil {
			continue
		}
		var num [12]byte
		if _, err := ioctl(h, ioctlStorageDeviceNum, nil, num[:]); err == nil {
			if di, ok := byNumber[le32(num[4:])]; ok {
				info.Disks[di].Letters = append(info.Disks[di].Letters, string(l)+":")
			}
		}
		syscall.CloseHandle(h)
	}
}

// storageDescriptor returns "Vendor Product" and the bus name.
func storageDescriptor(h syscall.Handle) (model, bus string) {
	var q [12]byte // STORAGE_PROPERTY_QUERY: PropertyId, QueryType, 1 spare byte
	out := make([]byte, 1024)
	n, err := ioctl(h, ioctlStorageQueryProp, q[:], out)
	if err != nil || n < 32 {
		return "", ""
	}
	str := func(off uint32) string {
		if off == 0 || int(off) >= len(out) {
			return ""
		}
		end := int(off)
		for end < len(out) && out[end] != 0 {
			end++
		}
		return strings.Join(strings.Fields(string(out[off:end])), " ")
	}
	model = strings.TrimSpace(str(le32(out[12:])) + " " + str(le32(out[16:])))
	switch le32(out[28:]) {
	case 1:
		bus = "SCSI"
	case 3:
		bus = "ATA"
	case 7:
		bus = "USB"
	case 8:
		bus = "RAID"
	case 10:
		bus = "SAS"
	case 11:
		bus = "SATA"
	case 12:
		bus = "SD"
	case 14, 15:
		bus = "Virtual"
	case 16:
		bus = "Storage Spaces"
	case 17:
		bus = "NVMe"
	}
	return model, bus
}

func diskKind(h syscall.Handle, bus string) string {
	if bus == "NVMe" {
		return "SSD"
	}
	var q [12]byte
	le := q[:]
	le[0] = storageSeekPenaltyProp
	out := make([]byte, 16)
	if n, err := ioctl(h, ioctlStorageQueryProp, le, out); err == nil && n >= 9 {
		if out[8] == 0 {
			return "SSD"
		}
		return "HDD"
	}
	return ""
}

func readDiskPerf(h syscall.Handle) (dp diskPerf, ok bool) {
	var b [88]byte
	if n, err := ioctl(h, ioctlDiskPerformance, nil, b[:]); err != nil || n < 72 {
		return dp, false
	}
	return diskPerf{
		bytesRead: le64(b[0:]), bytesWritten: le64(b[8:]),
		readTime: le64(b[16:]), writeTime: le64(b[24:]), idleTime: le64(b[32:]),
		readCount: le32(b[40:]), writeCount: le32(b[44:]), queueDepth: le32(b[48:]),
		queryTime: le64(b[56:]),
	}, true
}

func (p *platform) sampleDisks(s *Sample, info *Info) {
	s.Disks = make([]DiskSample, len(p.disks))
	for i, d := range p.disks {
		cur, ok := readDiskPerf(d.h)
		if !ok {
			d.ok = false
			continue
		}
		ds := &s.Disks[i]
		ds.Queue, ds.ReadBytes, ds.WriteBytes = cur.queueDepth, cur.bytesRead, cur.bytesWritten
		if d.ok && cur.queryTime > d.prev.queryTime {
			dt := float64(cur.queryTime - d.prev.queryTime) // 100 ns units
			sec := dt / 1e7
			ds.ReadRate = uint64(float64(cur.bytesRead-d.prev.bytesRead) / sec)
			ds.WriteRate = uint64(float64(cur.bytesWritten-d.prev.bytesWritten) / sec)
			busy := float64(cur.readTime-d.prev.readTime) + float64(cur.writeTime-d.prev.writeTime)
			if cur.idleTime == 0 && d.prev.idleTime == 0 {
				// The driver does not track idle time — fall back to
				// summed request time, which over-counts under queueing.
				ds.Active = clampPct(busy / dt * 100)
			} else {
				ds.Active = clampPct((1 - float64(cur.idleTime-d.prev.idleTime)/dt) * 100)
			}
			if n := (cur.readCount - d.prev.readCount) + (cur.writeCount - d.prev.writeCount); n > 0 {
				ds.RespMs = busy / float64(n) / 1e4
			}
		}
		d.prev, d.ok = cur, true
	}
}
