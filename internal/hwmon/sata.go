package hwmon

import "fmt"

// SATAAttribute is one SMART attribute: its id, the normalized current and
// worst values, and the six raw bytes the vendor defines.
type SATAAttribute struct {
	ID      int
	Current int
	Worst   int
	Raw     uint64
}

// SATAHealth is the decoded ATA SMART attribute table. Unlike NVMe, the
// attribute numbers and their meaning are vendor-defined, so the numbers
// below are the widely agreed readings; anything not listed here is still
// kept in Attributes as a raw value.
type SATAHealth struct {
	OK         bool
	Attributes []SATAAttribute
	// BelowThreshold is SMART's own "this attribute is failing now".
	BelowThreshold bool

	Reallocated       uint64 // 0x05
	PowerOnHours      uint64 // 0x09
	PowerCycles       uint64 // 0x0C
	ReportedUncorrect uint64 // 0xBB (187)
	Temperature       uint64 // 0xC2 (194), low byte, °C
	Pending           uint64 // 0xC5 (197)
	OfflineUncorrect  uint64 // 0xC6 (198)
	UDMACRC           uint64 // 0xC7 (199)
	TotalWritten      uint64 // 0xF1 (241), LBAs
	TotalRead         uint64 // 0xF2 (242), LBAs
	LifeLeft          int    // 0xE7 (231) normalized: % remaining
	Wearout           int    // 0xE9 (233) normalized: % remaining
}

// parseSATAHealth decodes the 512-byte SMART READ DATA reply and the
// matching READ THRESHOLDS reply (which may be nil). The attribute table
// starts at byte 2: 30 entries of 12 bytes.
func parseSATAHealth(data, thresholds []byte) *SATAHealth {
	if len(data) < 362 {
		return nil
	}
	thr := map[int]int{}
	if len(thresholds) >= 362 {
		for i := 0; i < 30; i++ {
			e := thresholds[2+i*12:]
			if e[0] != 0 {
				thr[int(e[0])] = int(e[3])
			}
		}
	}
	h := &SATAHealth{OK: true}
	for i := 0; i < 30; i++ {
		e := data[2+i*12 : 2+i*12+12]
		id := int(e[0])
		if id == 0 {
			continue
		}
		attr := SATAAttribute{ID: id, Current: int(e[3]), Worst: int(e[4]), Raw: le48(e[5:11])}
		h.Attributes = append(h.Attributes, attr)
		if t, ok := thr[id]; ok && t > 0 && attr.Current <= t {
			h.BelowThreshold = true
		}
		switch id {
		case 5:
			h.Reallocated = attr.Raw
		case 9:
			h.PowerOnHours = attr.Raw
		case 12:
			h.PowerCycles = attr.Raw
		case 187:
			h.ReportedUncorrect = attr.Raw
		case 194:
			h.Temperature = attr.Raw & 0xFF // low byte is °C on most drives
		case 197:
			h.Pending = attr.Raw
		case 198:
			h.OfflineUncorrect = attr.Raw
		case 199:
			h.UDMACRC = attr.Raw
		case 231:
			h.LifeLeft = attr.Current
		case 233:
			h.Wearout = attr.Current
		case 241:
			h.TotalWritten = attr.Raw
		case 242:
			h.TotalRead = attr.Raw
		}
	}
	return h
}

// JudgeSATAHealth gives a verdict from the attributes that are consistent
// across vendors. Wear attributes (231/233) are shown but only counted when
// the drive reports them, because their meaning is not standardized.
func JudgeSATAHealth(h *SATAHealth) (status string, reasons []string) {
	if h == nil || !h.OK {
		return "", nil
	}
	if h.Pending > 0 {
		reasons = append(reasons, fmt.Sprintf("%d sectors are pending reallocation", h.Pending))
	}
	if h.OfflineUncorrect > 0 {
		reasons = append(reasons, fmt.Sprintf("%d offline uncorrectable %s", h.OfflineUncorrect, pluralWord(h.OfflineUncorrect, "sector", "sectors")))
	}
	if h.BelowThreshold {
		reasons = append(reasons, "an attribute is at or below its failure threshold")
	}
	if len(reasons) > 0 {
		return "Replace", reasons
	}
	if h.Reallocated > 0 {
		reasons = append(reasons, fmt.Sprintf("%d sectors were reallocated", h.Reallocated))
	}
	if h.ReportedUncorrect > 0 {
		reasons = append(reasons, fmt.Sprintf("%d uncorrectable errors were reported", h.ReportedUncorrect))
	}
	if h.LifeLeft > 0 && h.LifeLeft <= 10 {
		reasons = append(reasons, fmt.Sprintf("the drive reports %d%% of its rated life left", h.LifeLeft))
	}
	if h.Wearout > 0 && h.Wearout <= 10 {
		reasons = append(reasons, fmt.Sprintf("the drive reports %d%% of its endurance remaining", h.Wearout))
	}
	if len(reasons) > 0 {
		return "Caution", reasons
	}
	return "Good", nil
}

// le48 reads SMART's six raw bytes as a little-endian integer.
func le48(b []byte) uint64 {
	var v uint64
	for i := min(5, len(b)-1); i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}
