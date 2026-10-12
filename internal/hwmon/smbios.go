package hwmon

import (
	"encoding/binary"
	"strings"
)

// smbios is what the app reads from the firmware's SMBIOS table.
type smbios struct {
	BIOSVendor, BIOSVersion, BIOSDate string
	SysMfr, SysProduct                string
	BoardMfr, BoardProduct            string
	Modules                           []DIMM
	Slots                             int
	MaxCapacity                       uint64
}

// parseSMBIOS walks a RawSMBIOSData blob (Windows' GetSystemFirmwareTable
// 'RSMB' layout: an 8-byte header, then the structure table). Structures
// are a formatted area of header-declared length followed by a string set
// ending in a double NUL; every read is bounds-checked because firmware
// tables are routinely short or odd.
func parseSMBIOS(raw []byte) smbios {
	var out smbios
	if len(raw) < 8 {
		return out
	}
	tableLen := int(binary.LittleEndian.Uint32(raw[4:]))
	end := len(raw)
	if tableLen > 0 && 8+tableLen < end {
		end = 8 + tableLen
	}
	raw = raw[:end]
	for p := 8; p+4 <= len(raw); {
		typ, l := raw[p], int(raw[p+1])
		if l < 4 || p+l > len(raw) {
			break
		}
		f := raw[p : p+l]
		strs, next := smbiosStrings(raw, p+l)
		str := func(off int) string {
			if off >= len(f) {
				return ""
			}
			i := int(f[off])
			if i == 0 || i > len(strs) {
				return ""
			}
			return cleanFirmware(strs[i-1])
		}
		u16 := func(off int) int {
			if off+2 > len(f) {
				return 0
			}
			return int(binary.LittleEndian.Uint16(f[off:]))
		}
		u32 := func(off int) uint32 {
			if off+4 > len(f) {
				return 0
			}
			return binary.LittleEndian.Uint32(f[off:])
		}
		switch typ {
		case 0:
			out.BIOSVendor, out.BIOSVersion, out.BIOSDate = str(4), str(5), str(8)
		case 1:
			out.SysMfr, out.SysProduct = str(4), str(5)
		case 2:
			out.BoardMfr, out.BoardProduct = str(4), str(5)
		case 16:
			out.Slots += u16(13)
			if mc := u32(7); mc == 0x80000000 && len(f) >= 23 {
				out.MaxCapacity += binary.LittleEndian.Uint64(f[15:])
			} else {
				out.MaxCapacity += uint64(mc) * 1024
			}
		case 17:
			if d, ok := parseDIMM(f, str, u16, u32); ok {
				out.Modules = append(out.Modules, d)
			}
		case 127:
			return out
		}
		p = next
	}
	return out
}

func parseDIMM(f []byte, str func(int) string, u16 func(int) int, u32 func(int) uint32) (DIMM, bool) {
	var size uint64
	switch sz := u16(12); {
	case sz == 0, sz == 0xFFFF:
		return DIMM{}, false // empty slot or unknown
	case sz == 0x7FFF:
		size = uint64(u32(28)&0x7FFFFFFF) << 20
	case sz&0x8000 != 0:
		size = uint64(sz&0x7FFF) << 10
	default:
		size = uint64(sz) << 20
	}
	d := DIMM{Size: size, Slot: str(16), Bank: str(17), Manufacturer: str(23), PartNo: str(26)}
	if len(f) > 14 {
		d.FormFactor = dimmForm(f[14])
	}
	if len(f) > 18 {
		d.Type = memType(f[18])
	}
	d.SpeedMTs = u16(21)
	if d.SpeedMTs == 0xFFFF {
		d.SpeedMTs = int(u32(84))
	}
	d.ConfigMTs = u16(32)
	if d.ConfigMTs == 0xFFFF {
		d.ConfigMTs = int(u32(88))
	}
	return d, true
}

// smbiosStrings reads the string set after a structure's formatted area
// and returns the offset of the next structure.
func smbiosStrings(raw []byte, start int) (strs []string, next int) {
	if start+1 < len(raw) && raw[start] == 0 && raw[start+1] == 0 {
		return nil, start + 2
	}
	i := start
	for i < len(raw) {
		j := i
		for j < len(raw) && raw[j] != 0 {
			j++
		}
		if j == i {
			return strs, i + 1
		}
		strs = append(strs, string(raw[i:j]))
		i = j + 1
	}
	return strs, i
}

// cleanFirmware trims the padding and placeholder strings OEMs leave in
// SMBIOS fields.
func cleanFirmware(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "to be filled by o.e.m.", "default string", "not specified", "unknown", "system manufacturer",
		"system product name", "none", "n/a", "null", "o.e.m.", "oem":
		return ""
	}
	return s
}

func memType(t byte) string {
	switch t {
	case 0x12:
		return "DDR"
	case 0x13:
		return "DDR2"
	case 0x18:
		return "DDR3"
	case 0x1A:
		return "DDR4"
	case 0x1B:
		return "LPDDR"
	case 0x1C:
		return "LPDDR2"
	case 0x1D:
		return "LPDDR3"
	case 0x1E:
		return "LPDDR4"
	case 0x22:
		return "DDR5"
	case 0x23:
		return "LPDDR5"
	}
	return ""
}

func dimmForm(f byte) string {
	switch f {
	case 0x09:
		return "DIMM"
	case 0x0D:
		return "SODIMM"
	case 0x0F:
		return "FB-DIMM"
	case 0x0C:
		return "RIMM"
	}
	return ""
}
