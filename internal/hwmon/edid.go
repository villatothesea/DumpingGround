package hwmon

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// parseEDID decodes the 128-byte base EDID block a monitor reports: the
// PNP manufacturer id, the monitor-name and serial descriptors, the panel
// size in centimetres and the native mode from the first detailed timing
// descriptor.
func parseEDID(b []byte) (DisplayInfo, bool) {
	if len(b) < 128 || b[0] != 0x00 || b[1] != 0xFF || b[6] != 0xFF || b[7] != 0x00 {
		return DisplayInfo{}, false
	}
	d := DisplayInfo{
		Manufacturer: pnpID(b[8], b[9]),
		WidthCM:      int(b[21]),
		HeightCM:     int(b[22]),
		Year:         1990 + int(b[17]),
	}
	if t := b[54:72]; t[0] != 0 || t[1] != 0 {
		d.NativeWidth = int(t[2]) | int(t[4]>>4)<<8
		d.NativeHeight = int(t[5]) | int(t[7]>>4)<<8
	}
	for off := 54; off+18 <= 126; off += 18 {
		desc := b[off : off+18]
		if desc[0] != 0 || desc[1] != 0 || desc[2] != 0 {
			continue
		}
		txt := strings.TrimSpace(strings.TrimRight(string(desc[5:18]), "\x00\n\r "))
		switch desc[3] {
		case 0xFC: // monitor name
			if d.Model == "" {
				d.Model = txt
			}
		case 0xFF: // serial number
			if d.Serial == "" {
				d.Serial = txt
			}
		}
	}
	if d.Model == "" {
		d.Model = fmt.Sprintf("%s %04X", d.Manufacturer, binary.BigEndian.Uint16(b[10:12]))
	}
	return d, true
}

// pnpID expands EDID's three packed 5-bit letters (1 = 'A').
func pnpID(hi, lo byte) string {
	v := int(hi)<<8 | int(lo)
	out := make([]byte, 3)
	for i := 0; i < 3; i++ {
		c := (v >> (10 - 5*i)) & 0x1F
		if c == 0 {
			out[i] = '?'
			continue
		}
		out[i] = byte('A' + c - 1)
	}
	return string(out)
}
