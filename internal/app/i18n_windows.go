//go:build windows

package app

import "syscall"

var procUserUILang = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// systemLang maps the Windows UI language onto the nearest supported
// language id. LANGIDs: 0x0804 zh-CN, 0x0404/0x0c04/0x1004 zh-TW and
// other traditional locales, 0x0411 ja, 0x0412 ko.
func systemLang() int {
	langID, _, _ := procUserUILang.Call()
	switch uint16(langID) & 0x3ff { // primary language id
	case 0x04: // Chinese — traditional sublanguage ids select zh-TW
		switch uint16(langID) {
		case 0x0404, 0x0c04, 0x1004, 0x048e:
			return langZhTW
		}
		return langZhCN
	case 0x11:
		return langJA
	case 0x12:
		return langKO
	}
	return langEN
}
