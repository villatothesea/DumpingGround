//go:build windows

package hwmon

import (
	"encoding/binary"
	"syscall"
)

const (
	ioctlSmartRcvDriveData = 0x7C088 // SMART_RCV_DRIVE_DATA

	smartCmdReadData       = 0xD0
	smartCmdReadThresholds = 0xD1
	smartCommand           = 0xB0
	smartLbaMid            = 0x4F
	smartLbaHigh           = 0xC2

	sendCmdInHeader  = 32 // offset of SENDCMDINPARAMS.bBuffer
	sendCmdOutHeader = 16 // offset of SENDRESULT.bBuffer
	smartBufferSize  = 512
)

// readSATASmart asks the ATA driver for the drive's SMART data and
// thresholds through SMART_RCV_DRIVE_DATA. A drive that does not answer
// (NVMe, many USB bridges, or no rights) returns nil rather than a fake
// all-clear.
func readSATASmart(h syscall.Handle) *SATAHealth {
	data, ok := smartRead(h, smartCmdReadData)
	if !ok {
		return nil
	}
	thr, _ := smartRead(h, smartCmdReadThresholds)
	return parseSATAHealth(data, thr)
}

func smartRead(h syscall.Handle, feature byte) ([]byte, bool) {
	// SENDCMDINPARAMS: cBufferSize, IDEREGS, bDriveNumber, padding, then
	// the 512-byte data buffer.
	in := make([]byte, sendCmdInHeader+smartBufferSize)
	binary.LittleEndian.PutUint32(in, smartBufferSize) // cBufferSize
	ir := in[4:]                                       // IDEREGS
	ir[0] = feature                                    // bFeaturesReg: D0h read data, D1h read thresholds
	ir[1] = 0x01                                       // bSectorCountReg
	ir[2] = 0x01                                       // bSectorNumberReg
	ir[3] = smartLbaMid                                // bCylLowReg
	ir[4] = smartLbaHigh                               // bCylHighReg
	ir[5] = 0xA0                                       // bDriveHeadReg
	ir[6] = smartCommand                               // bCommandReg: B0h = SMART
	out := make([]byte, sendCmdOutHeader+smartBufferSize)
	n, err := ioctl(h, ioctlSmartRcvDriveData, in, out)
	if err != nil || n < uint32(sendCmdOutHeader+smartBufferSize) {
		return nil, false
	}
	return out[sendCmdOutHeader : sendCmdOutHeader+smartBufferSize], true
}

// openReadWrite opens a drive handle the SMART IOCTL requires: unlike the
// performance and query IOCTLs, it needs read and write access.
func openReadWrite(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
}
