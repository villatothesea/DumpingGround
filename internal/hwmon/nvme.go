package hwmon

import (
	"encoding/binary"
	"fmt"
)

// DiskHealth is the NVMe SMART/Health Information log (log page 0x02). The
// fields are defined by the NVMe specification, so they mean the same thing
// on every drive — unlike the vendor-specific SATA attribute numbers.
type DiskHealth struct {
	OK                      bool
	CriticalWarning         uint8 // bit field: spare low, temperature, reliability, read-only, backup
	TempC                   int   // composite temperature, °C; 0 when the drive reports none
	AvailableSpare          int   // % of spare blocks left
	AvailableSpareThreshold int   // % below which the drive itself warns
	PercentageUsed          int   // % of the rated endurance consumed
	DataRead                uint64
	DataWritten             uint64
	ControllerBusyMinutes   uint64
	PowerCycles             uint64
	PowerOnHours            uint64
	UnsafeShutdowns         uint64
	MediaErrors             uint64
	ErrorLogEntries         uint64
	WarningTempMinutes      uint64
	CriticalTempMinutes     uint64
}

var criticalWarningBits = []struct {
	bit  uint8
	text string
}{
	{0x01, "spare blocks at or below threshold"},
	{0x02, "temperature outside the safe range"},
	{0x04, "subsystem reliability degraded"},
	{0x08, "media placed in read-only mode"},
	{0x10, "volatile memory backup failed"},
}

// JudgeHealth turns a reading into a verdict using only the NVMe spec's own
// criteria — there is deliberately no invented "health score". It returns
// "Good", "Caution" or "Replace" (empty when there is no reading) and the
// observations behind the verdict.
func JudgeHealth(h *DiskHealth) (status string, reasons []string) {
	if h == nil || !h.OK {
		return "", nil
	}
	if h.CriticalWarning != 0 {
		for _, b := range criticalWarningBits {
			if h.CriticalWarning&b.bit != 0 {
				reasons = append(reasons, "critical warning: "+b.text)
			}
		}
		if len(reasons) == 0 {
			reasons = append(reasons, fmt.Sprintf("critical warning 0x%02x", h.CriticalWarning))
		}
	}
	if h.AvailableSpareThreshold > 0 && h.AvailableSpare <= h.AvailableSpareThreshold {
		reasons = append(reasons, fmt.Sprintf("available spare %d%% is at or below the drive's %d%% threshold", h.AvailableSpare, h.AvailableSpareThreshold))
	}
	if len(reasons) > 0 {
		return "Replace", reasons
	}
	if h.PercentageUsed >= 80 {
		reasons = append(reasons, fmt.Sprintf("%d%% of the rated endurance is used", h.PercentageUsed))
	}
	if h.MediaErrors > 0 {
		reasons = append(reasons, fmt.Sprintf("%d media/data-integrity %s recorded", h.MediaErrors, pluralWord(h.MediaErrors, "error", "errors")))
	}
	if h.CriticalTempMinutes > 0 {
		reasons = append(reasons, fmt.Sprintf("%d minutes spent above the critical temperature", h.CriticalTempMinutes))
	} else if h.WarningTempMinutes > 0 {
		reasons = append(reasons, fmt.Sprintf("%d minutes spent above the warning temperature", h.WarningTempMinutes))
	}
	if len(reasons) > 0 {
		return "Caution", reasons
	}
	return "Good", nil
}

func pluralWord(n uint64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// parseNVMeHealthLog decodes the 512-byte log page; offsets are from the
// NVMe base specification (Figure "SMART / Health Information Log").
func parseNVMeHealthLog(b []byte) (DiskHealth, bool) {
	if len(b) < 200 {
		return DiskHealth{}, false
	}
	h := DiskHealth{OK: true}
	h.CriticalWarning = b[0]
	if t := binary.LittleEndian.Uint16(b[1:]); t > 0 {
		h.TempC = int(t) - 273 // the log stores Kelvin
	}
	h.AvailableSpare = int(b[3])
	h.AvailableSpareThreshold = int(b[4])
	h.PercentageUsed = int(b[5])
	// Data-unit counters count 1000 × 512-byte units; the 128-bit values
	// are read as their low 64 bits, which cannot overflow in practice.
	h.DataRead = leUint(b[32:]) * 1000 * 512
	h.DataWritten = leUint(b[48:]) * 1000 * 512
	h.ControllerBusyMinutes = leUint(b[96:])
	h.PowerCycles = leUint(b[112:])
	h.PowerOnHours = leUint(b[128:])
	h.UnsafeShutdowns = leUint(b[144:])
	h.MediaErrors = leUint(b[160:])
	h.ErrorLogEntries = leUint(b[176:])
	h.WarningTempMinutes = uint64(binary.LittleEndian.Uint32(b[192:]))
	h.CriticalTempMinutes = uint64(binary.LittleEndian.Uint32(b[196:]))
	return h, true
}

// leUint reads a little-endian integer of up to 16 bytes (the spec's
// 128-bit counters) as a uint64, ignoring anything above the low 64 bits.
func leUint(b []byte) uint64 {
	return binary.LittleEndian.Uint64(b[:8])
}
