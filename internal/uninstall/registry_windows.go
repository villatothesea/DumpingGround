//go:build windows

package uninstall

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	advapi            = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyEx  = advapi.NewProc("RegOpenKeyExW")
	procRegCloseKey   = advapi.NewProc("RegCloseKey")
	procRegEnumKeyEx  = advapi.NewProc("RegEnumKeyExW")
	procRegEnumValue  = advapi.NewProc("RegEnumValueW")
	procRegQueryValue = advapi.NewProc("RegQueryValueExW")
	procRegQueryInfo  = advapi.NewProc("RegQueryInfoKeyW")
	procRegCreateKey  = advapi.NewProc("RegCreateKeyExW")
	procRegSetValue   = advapi.NewProc("RegSetValueExW")
	msiDLL            = syscall.NewLazyDLL("msi.dll")
	procMsiInfo       = msiDLL.NewProc("MsiGetProductInfoW")
)

const (
	hkeyLocalMachine  = 0x80000002
	hkeyCurrentUser   = 0x80000001
	keyRead           = 0x20019          // KEY_READ
	keyWow64          = 0x20019 | 0x0100 // KEY_READ | KEY_WOW64_32KEY
	regSz             = 1
	regExpandSz       = 2
	regDword          = 4
	regMultiSz        = 7
	regQword          = 11
	errorMoreData     = 234
	errorFileNotFound = 2
)

// hives are the three Uninstall keys Windows itself merges: 64-bit
// machine, 32-bit machine (WOW6432Node) and per-user.
var hives = []struct {
	root   uintptr
	access uintptr
	sub    string
	system bool
}{
	{hkeyLocalMachine, keyRead, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, true},
	{hkeyLocalMachine, keyWow64, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, true},
	{hkeyCurrentUser, keyRead, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, false},
}

type regKey uintptr

func openKey(root, access uintptr, sub string) (regKey, error) {
	p, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return 0, err
	}
	var h uintptr
	r, _, _ := procRegOpenKeyEx.Call(root, uintptr(unsafe.Pointer(p)), 0, access, uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return regKey(h), nil
}

func (k regKey) close() {
	procRegCloseKey.Call(uintptr(k))
}

// writeTime returns the key's last write time. Installers stamp their
// Uninstall key when they install (or upgrade), so it stands in for
// InstallDate when the app never wrote one.
func (k regKey) writeTime() (t syscall.Filetime, ok bool) {
	r, _, _ := procRegQueryInfo.Call(uintptr(k), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&t)))
	return t, r == 0
}

// subKeys yields the immediate child key names.
func (k regKey) subKeys() ([]string, error) {
	var out []string
	for i := uint32(0); ; i++ {
		buf := make([]uint16, 256)
		n := uint32(len(buf))
		r, _, _ := procRegEnumKeyEx.Call(uintptr(k), uintptr(i),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)),
			0, 0, 0, 0)
		if r == errorFileNotFound || r == 259 /*ERROR_NO_MORE_ITEMS*/ {
			return out, nil
		}
		if r != 0 {
			return out, syscall.Errno(r)
		}
		out = append(out, syscall.UTF16ToString(buf[:n]))
	}
}

// values reads a key's value set into a name→(data, kind) map.
func (k regKey) values() (regValues, error) {
	out := regValues{}
	for i := uint32(0); ; i++ {
		var name [256]uint16
		nlen := uint32(len(name))
		var kind, dlen uint32
		// First call sizes the data buffer.
		r, _, _ := procRegEnumValue.Call(uintptr(k), uintptr(i),
			uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nlen)),
			0, uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&dlen)))
		if r == 259 {
			return out, nil
		}
		if r == errorMoreData || r == 0 {
			data := make([]byte, max(dlen, 1))
			nlen = uint32(len(name))
			r, _, _ = procRegEnumValue.Call(uintptr(k), uintptr(i),
				uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nlen)),
				0, uintptr(unsafe.Pointer(&kind)),
				uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dlen)))
			if r == 259 {
				return out, nil
			}
			if r != 0 {
				return out, syscall.Errno(r)
			}
			out[syscall.UTF16ToString(name[:nlen])] = regValue{kind: kind, data: data[:dlen]}
		} else if r != 0 {
			return out, syscall.Errno(r)
		}
	}
}

type regValues map[string]regValue

type regValue struct {
	kind uint32
	data []byte
}

func (v regValue) str() string {
	if v.kind != regSz && v.kind != regExpandSz || len(v.data) < 2 {
		return ""
	}
	n := len(v.data) / 2
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&v.data[0])), n)
	if n > 0 && u16[n-1] == 0 {
		u16 = u16[:n-1]
	}
	return strings.TrimSpace(syscall.UTF16ToString(u16))
}

func (v regValue) dword() uint64 {
	if v.kind == regDword && len(v.data) >= 4 {
		return uint64(*(*uint32)(unsafe.Pointer(&v.data[0])))
	}
	if v.kind == regQword && len(v.data) >= 8 {
		return *(*uint64)(unsafe.Pointer(&v.data[0]))
	}
	return 0
}

func (vals regValues) str(name string) string {
	return vals[name].str()
}

// enumUninstall reads every Uninstall subkey from all three hives.
func enumUninstall() []Entry {
	var out []Entry
	for _, h := range hives {
		root, err := openKey(h.root, h.access, h.sub)
		if err != nil {
			continue
		}
		names, err := root.subKeys()
		if err != nil {
			root.close()
			continue
		}
		for _, name := range names {
			k, err := openKey(h.root, h.access, h.sub+`\`+name)
			if err != nil {
				continue
			}
			vals, err := k.values()
			stamp, stampOK := k.writeTime()
			k.close()
			if err != nil {
				continue
			}
			// SystemComponent entries are runtimes, patches and driver
			// bits — the Apps page hides them too.
			if vals["SystemComponent"].dword() == 1 {
				continue
			}
			e := Entry{
				Name:        vals["DisplayName"].str(),
				Version:     vals["DisplayVersion"].str(),
				Publisher:   vals["Publisher"].str(),
				InstallDate: vals["InstallDate"].str(),
				Location:    vals["InstallLocation"].str(),
				Uninstall:   vals["UninstallString"].str(),
				QuietUninst: vals["QuietUninstallString"].str(),
				Icon:        vals["DisplayIcon"].str(),
				SystemScope: h.system,
				Key:         h.sub + `\` + name,
				Wow32:       h.access&0x100 != 0,
				Hive:        map[bool]string{true: "HKLM", false: "HKCU"}[h.system],
			}
			// Installers store these quoted or with %VARS% — normalize
			// once so open-folder, footprint and icon code stay simple.
			e.Location = regPath(e.Location)
			if e.Location != "" && !SaneLocation(e.Location) {
				e.Location = "" // e.g. "C:\Program Files\" itself
			}
			if e.Location == "" {
				// InstallSource is the installer *media* dir, not the
				// install dir — the uninstall string's own folder is a
				// better guess for per-user Inno/NSIS installs. An
				// icon-derived dir is a guess, not evidence.
				if d, guessed := e.locationFallback(); d != "" {
					e.Location, e.LocationGuessed = d, guessed
				}
			}
			if e.Location == "" {
				e.Location = msiInstallLocation(e.Uninstall)
			}
			e.Icon = regPath(e.Icon)
			// Windows Installer keeps its own copy of the metadata
			// under Installer\UserData — MSI entries that left
			// InstallLocation/DisplayIcon/InstallDate empty in the
			// Uninstall key often have them there.
			if e.Location == "" || e.Icon == "" || e.InstallDate == "" {
				if props := msiProps(e.Uninstall); props != nil {
					if e.Location == "" {
						e.Location = regPath(props["InstallLocation"].str())
					}
					if e.Icon == "" {
						e.Icon = regPath(props["DisplayIcon"].str())
					}
					if e.InstallDate == "" {
						e.InstallDate = props["InstallDate"].str()
					}
				}
			}
			if e.Location == "" {
				e.Location = e.programFilesLocation()
			}
			if e.InstallDate == "" && stampOK {
				e.InstallDate = time.Unix(0, stamp.Nanoseconds()).Format("20060102")
				e.DateEstimated = true
			}
			if e.Location == "" {
				// Custom-path installs (Bitwig) record nothing — a
				// shortcut named after the app points back at it.
				if e.Location = e.shortcutLocation(); e.Location != "" {
					e.LocationGuessed = true
				}
			}
			if e.Location == "" || e.Icon == "" {
				// File associations and App Paths carry exe/icon
				// paths for apps whose uninstall record is bare.
				if d := e.appPathsLocation(); e.Location == "" && d != "" {
					e.Location, e.LocationGuessed = d, true
				}
				d, ic := e.assocLocation()
				if e.Location == "" && d != "" {
					e.Location, e.LocationGuessed = d, true
				}
				if e.Icon == "" && ic != "" {
					e.Icon = ic
				}
			}
			if e.Location != "" && !SaneLocation(e.Location) {
				e.Location = "" // a fallback found a root, not a folder
			}
			// Installers write InstallLocation with and without a
			// trailing slash; dedupe and display treat them as one.
			if len(e.Location) > 3 {
				e.Location = strings.TrimSuffix(e.Location, `\`)
			}
			if sz := vals["EstimatedSize"].dword(); sz > 0 {
				e.SizeBytes = sz * 1024 // EstimatedSize is kilobytes
				e.EstimatedSize = true
			}
			out = append(out, e)
		}
		root.close()
	}
	return out
}

func (e Entry) String() string { return fmt.Sprintf("%s %s", e.Name, e.Version) }

// msiInstallLocation asks Windows Installer where a product lives —
// MSI entries never write InstallLocation, and their MsiExec uninstall
// string reveals nothing about the folder.
func msiInstallLocation(uninstall string) string {
	start := strings.Index(uninstall, "{")
	end := strings.Index(uninstall, "}")
	if start < 0 || end <= start {
		return ""
	}
	guid, err1 := syscall.UTF16PtrFromString(uninstall[start : end+1])
	prop, err2 := syscall.UTF16PtrFromString("InstallLocation")
	if err1 != nil || err2 != nil {
		return ""
	}
	buf := make([]uint16, 512)
	n := uint32(len(buf))
	r, _, _ := procMsiInfo.Call(uintptr(unsafe.Pointer(guid)), uintptr(unsafe.Pointer(prop)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r != 0 || n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// msiProps reads a product's InstallProperties from Windows Installer's
// own store, HKLM\...\Installer\UserData\<SID>\Products\<packed GUID>.
// The Uninstall key's {GUID} name is the product code in display form;
// MSI names its keys by the compressed form, which packGUID computes.
func msiProps(uninstall string) regValues {
	start := strings.Index(uninstall, "{")
	end := strings.Index(uninstall, "}")
	if start < 0 || end <= start {
		return nil
	}
	packed := packGUID(uninstall[start : end+1])
	if packed == "" {
		return nil
	}
	const userData = `SOFTWARE\Microsoft\Windows\CurrentVersion\Installer\UserData`
	ud, err := openKey(hkeyLocalMachine, keyRead, userData)
	if err != nil {
		return nil
	}
	defer ud.close()
	for _, sid := range mustSubKeys(ud) {
		k, err := openKey(hkeyLocalMachine, keyRead,
			userData+`\`+sid+`\Products\`+packed+`\InstallProperties`)
		if err != nil {
			continue
		}
		vals, err := k.values()
		k.close()
		if err == nil {
			return vals
		}
	}
	return nil
}

// packGUID reorders a {GUID} into MSI's compressed registry name: the
// first three fields reverse byte order, the last two swap the nibbles
// within each byte. {AC0DDDDD-1146-11D4-B4A0-006098C2901C} becomes
// DDDD0DAC4611D4114B0A0006892C09C1.
func packGUID(g string) string {
	g = strings.Trim(g, "{}")
	parts := strings.Split(g, "-")
	if len(parts) != 5 {
		return ""
	}
	rev := func(s string) string {
		var b strings.Builder
		for i := len(s); i > 0; i -= 2 {
			b.WriteString(s[i-2 : i])
		}
		return b.String()
	}
	nib := func(s string) string {
		var b strings.Builder
		for i := 0; i+1 < len(s); i += 2 {
			b.WriteByte(s[i+1])
			b.WriteByte(s[i])
		}
		return b.String()
	}
	return rev(parts[0]) + rev(parts[1]) + rev(parts[2]) + nib(parts[3]) + nib(parts[4])
}

// OpenInRegedit jumps Registry Editor to this entry's Uninstall key —
// Regedit restores its last position from the LastKey value.
func (e *Entry) OpenInRegedit() error {
	if e.Key == "" {
		return fmt.Errorf("no registry key")
	}
	sub := `Software\Microsoft\Windows\CurrentVersion\Applets\Regedit`
	p, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return err
	}
	var h uintptr
	r, _, _ := procRegCreateKey.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)),
		0, 0, 0, 0x2001F /*KEY_ALL_ACCESS*/, 0, uintptr(unsafe.Pointer(&h)), 0)
	if r != 0 {
		return syscall.Errno(r)
	}
	defer procRegCloseKey.Call(h)
	full := e.Hive + `\` + e.Key
	full = strings.Replace(full, "HKLM", "HKEY_LOCAL_MACHINE", 1)
	full = strings.Replace(full, "HKCU", "HKEY_CURRENT_USER", 1)
	name, _ := syscall.UTF16PtrFromString("LastKey")
	data, _ := syscall.UTF16FromString(full)
	r, _, _ = procRegSetValue.Call(h, uintptr(unsafe.Pointer(name)), 0,
		uintptr(regSz), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	cmd := exec.Command("regedit")
	hideWindow(cmd)
	return cmd.Start()
}
