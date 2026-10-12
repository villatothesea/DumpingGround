//go:build windows

package hwmon

import (
	"encoding/binary"
	"syscall"
)

const (
	// IOCTL_STORAGE_QUERY_PROPERTY with StorageDeviceProtocolSpecificProperty
	// is Windows' documented, driver-free way to read the NVMe log pages.
	// CrystalDiskInfo reads NVMe health the same way.
	storageDeviceProtocolSpecificProperty = 50 // STORAGE_PROPERTY_ID
	protocolTypeNvme                      = 3  // STORAGE_PROTOCOL_TYPE
	nvmeDataTypeLogPage                   = 2  // STORAGE_PROTOCOL_NVME_DATA_TYPE
	nvmeLogPageHealthInfo                 = 0x02

	// The request header is STORAGE_PROPERTY_QUERY (PropertyId, QueryType)
	// followed immediately by STORAGE_PROTOCOL_SPECIFIC_DATA — at byte 8,
	// where the query's AdditionalParameters field starts. The driver
	// returns a STORAGE_PROTOCOL_DATA_DESCRIPTOR that puts the same
	// structure at the same offset, with the log page after it.
	storageProtocolHeaderSize = 8
	storageProtocolDataSize   = 40 // sizeof(STORAGE_PROTOCOL_SPECIFIC_DATA)
	nvmeHealthLogSize         = 512
)

// readNVMeHealth issues the protocol-specific query and parses the health
// log. A drive that does not pass NVMe commands through (a USB bridge, or
// NVMe hidden behind Intel RST/VMD) fails the call; nil means "no reading",
// never a made-up all-clear.
func readNVMeHealth(h syscall.Handle) *DiskHealth {
	buf := make([]byte, storageProtocolHeaderSize+storageProtocolDataSize+nvmeHealthLogSize)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], storageDeviceProtocolSpecificProperty)
	le.PutUint32(buf[4:], 0) // PropertyStandardQuery
	pd := buf[storageProtocolHeaderSize:]
	le.PutUint32(pd[0:], protocolTypeNvme)
	le.PutUint32(pd[4:], nvmeDataTypeLogPage)
	le.PutUint32(pd[8:], nvmeLogPageHealthInfo) // ProtocolDataRequestValue
	le.PutUint32(pd[12:], 0)                    // ProtocolDataRequestSubValue
	le.PutUint32(pd[16:], storageProtocolDataSize)
	le.PutUint32(pd[20:], nvmeHealthLogSize)

	var ret uint32
	// The input is only the header; the output has room for the log page
	// the driver appends.
	if err := syscall.DeviceIoControl(h, ioctlStorageQueryProp, &buf[0], storageProtocolHeaderSize+storageProtocolDataSize, &buf[0], uint32(len(buf)), &ret, nil); err != nil {
		return nil
	}
	// ProtocolDataOffset is relative to the start of the protocol-data
	// structure, not the whole buffer.
	data := pd[storageProtocolDataSize:]
	if len(data) < nvmeHealthLogSize {
		return nil
	}
	dh, ok := parseNVMeHealthLog(data)
	if !ok {
		return nil
	}
	return &dh
}
