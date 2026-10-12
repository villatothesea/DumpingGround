package hwmon

import "testing"

func TestParseEDID(t *testing.T) {
	b := make([]byte, 128)
	b[0], b[1], b[6], b[7] = 0x00, 0xFF, 0xFF, 0x00
	// Manufacturer "DEL": D=4, E=5, L=12 packed as three 5-bit letters.
	v := (4 << 10) | (5 << 5) | 12
	b[8], b[9] = byte(v>>8), byte(v)
	b[10], b[11] = 0x41, 0xE2 // product code
	b[17] = 29                // 2019
	b[21], b[22] = 60, 34     // panel size, cm
	// Detailed timing descriptor: native 2560 × 1440.
	dtd := b[54:72]
	dtd[0], dtd[1] = 0x11, 0x00
	dtd[2], dtd[4] = 0x00, 0xA0 // 2560 = 0xA00
	dtd[5], dtd[7] = 0xA0, 0x50 // 1440 = 0x5A0
	// Monitor-name and serial descriptors.
	copy(b[72:], []byte{0, 0, 0, 0xFC, 0, 'S', '2', '7', '1', '9', 'D', 'G', 'F', '\n', ' ', ' ', ' ', ' '})
	copy(b[90:], []byte{0, 0, 0, 0xFF, 0, '7', 'J', 'Q', 'X', 'B', 'Y', '2', '\n', ' ', ' ', ' ', ' ', ' '})

	d, ok := parseEDID(b)
	if !ok {
		t.Fatal("a valid EDID block was rejected")
	}
	if d.Manufacturer != "DEL" || d.Model != "S2719DGF" || d.Serial != "7JQXBY2" {
		t.Errorf("identity = %q/%q/%q", d.Manufacturer, d.Model, d.Serial)
	}
	if d.NativeWidth != 2560 || d.NativeHeight != 1440 {
		t.Errorf("native = %dx%d", d.NativeWidth, d.NativeHeight)
	}
	if d.WidthCM != 60 || d.HeightCM != 34 || d.Year != 2019 {
		t.Errorf("size/year = %dx%d cm, %d", d.WidthCM, d.HeightCM, d.Year)
	}
	if _, ok := parseEDID(make([]byte, 64)); ok {
		t.Error("a truncated block parsed as valid")
	}
	if _, ok := parseEDID(make([]byte, 128)); ok {
		t.Error("an all-zero block parsed as valid")
	}
}
