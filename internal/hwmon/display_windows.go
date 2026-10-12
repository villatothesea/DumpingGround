//go:build windows

package hwmon

import (
	"encoding/binary"
	"strings"
	"syscall"
	"unsafe"
)

func loadDisplays(info *Info, present map[string]bool) {
	info.Displays = readEDIDs(present)
	info.DisplayMode = readDisplayMode()
}

// readEDIDs walks the registry copy of every attached monitor's EDID.
// Windows caches a block per display under Enum\DISPLAY, including ones
// that are no longer connected, so only instances present in the device
// tree are kept.
func readEDIDs(present map[string]bool) []DisplayInfo {
	root, ok := regOpen(`SYSTEM\CurrentControlSet\Enum\DISPLAY`)
	if !ok {
		return nil
	}
	defer syscall.RegCloseKey(root)
	var out []DisplayInfo
	seen := map[string]bool{}
	for _, mfr := range regSubkeys(root) {
		mh, ok := regOpenSub(root, mfr)
		if !ok {
			continue
		}
		for _, inst := range regSubkeys(mh) {
			if len(present) > 0 && !present[strings.ToLower(`display\`+mfr+`\`+inst)] {
				continue
			}
			ih, ok := regOpenSub(mh, inst)
			if !ok {
				continue
			}
			if ph, ok := regOpenSub(ih, "Device Parameters"); ok {
				if raw := regRead(ph, "EDID"); len(raw) >= 128 {
					if d, ok := parseEDID(raw); ok {
						key := d.Manufacturer + "|" + d.Model + "|" + d.Serial
						if !seen[key] {
							seen[key] = true
							out = append(out, d)
						}
					}
				}
				syscall.RegCloseKey(ph)
			}
			syscall.RegCloseKey(ih)
		}
		syscall.RegCloseKey(mh)
	}
	return out
}

// ENUM_CURRENT_SETTINGS asks for the mode the output is running now.
const enumCurrentSettings = 0xFFFFFFFF

// readDisplayMode reads the primary output's current mode. DEVMODEW's
// dmPelsWidth/dmPelsHeight/dmDisplayFrequency fields sit at these offsets.
func readDisplayMode() DisplayMode {
	var dm [220]byte
	binary.LittleEndian.PutUint16(dm[68:], 220) // dmSize
	if r, _, _ := procEnumDisplaySettings.Call(0, enumCurrentSettings, uintptr(unsafe.Pointer(&dm[0]))); r == 0 {
		return DisplayMode{}
	}
	return DisplayMode{
		Width:      int(le32(dm[172:])),
		Height:     int(le32(dm[176:])),
		BitsPerPel: int(le32(dm[168:])),
		RefreshHz:  int(le32(dm[184:])),
	}
}
