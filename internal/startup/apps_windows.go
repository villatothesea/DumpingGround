//go:build windows

package startup

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/dumpingground/dumpingground/internal/uninstall"
)

const (
	runPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	run32Path    = `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`
	approvedPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\`
)

var (
	procRegEnumValue   = advapi32.NewProc("RegEnumValueW")
	procRegSetValueEx  = advapi32.NewProc("RegSetValueExW")
	procRegCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")
)

// status turns a registry call's LSTATUS return into an error.
func status(r uintptr) error {
	if r == 0 {
		return nil
	}
	return syscall.Errno(r)
}

// regSet writes one registry value.
func regSet(h syscall.Handle, name string, typ uint32, data []byte) error {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r, _, _ := procRegSetValueEx.Call(uintptr(h), uintptr(unsafe.Pointer(n)), 0, uintptr(typ), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	return status(r)
}

var hiveRoot = map[string]syscall.Handle{"HKCU": syscall.HKEY_CURRENT_USER, "HKLM": syscall.HKEY_LOCAL_MACHINE}

func openKey(root syscall.Handle, path string, access uint32) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	err = syscall.RegOpenKeyEx(root, p, 0, access, &h)
	return h, err
}

func createKey(root syscall.Handle, path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	r, _, _ := procRegCreateKeyEx.Call(uintptr(root), uintptr(unsafe.Pointer(p)), 0, 0, 0,
		syscall.KEY_SET_VALUE|syscall.KEY_QUERY_VALUE, 0, uintptr(unsafe.Pointer(&h)), 0)
	return h, status(r)
}

type regValue struct {
	name string
	typ  uint32
	data []byte
}

// readValues lists a key's values; a missing key is simply empty.
func readValues(root syscall.Handle, path string) []regValue {
	h, err := openKey(root, path, syscall.KEY_QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer syscall.RegCloseKey(h)
	var n, maxName, maxData uint32
	if syscall.RegQueryInfoKey(h, nil, nil, nil, nil, nil, nil, &n, &maxName, &maxData, nil, nil) != nil {
		return nil
	}
	var out []regValue
	for i := uint32(0); i < n; i++ {
		name := make([]uint16, maxName+1)
		nl := uint32(len(name))
		data := make([]byte, maxData+2)
		dl := uint32(len(data))
		var typ uint32
		if r, _, _ := procRegEnumValue.Call(uintptr(h), uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nl)),
			0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dl))); r != 0 {
			continue
		}
		out = append(out, regValue{name: syscall.UTF16ToString(name[:nl]), typ: typ, data: data[:dl]})
	}
	return out
}

func queryValue(h syscall.Handle, name string) []byte {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil
	}
	var typ, size uint32
	if syscall.RegQueryValueEx(h, n, nil, &typ, nil, &size) != nil || size == 0 {
		return nil
	}
	buf := make([]byte, size)
	if syscall.RegQueryValueEx(h, n, nil, &typ, &buf[0], &size) != nil {
		return nil
	}
	return buf[:size]
}

// approvedDisabled reads a StartupApproved flag: the first byte's low bit
// set (03) means switched off; 02 or 06, or no value at all, means on.
func approvedDisabled(hive, sub, name string) bool {
	h, err := openKey(hiveRoot[hive], approvedPath+sub, syscall.KEY_QUERY_VALUE)
	if err != nil {
		return false
	}
	defer syscall.RegCloseKey(h)
	b := queryValue(h, name)
	return len(b) >= 4 && b[0]&1 == 1
}

// setApproved writes the 12-byte flag Task Manager writes: 02… to enable,
// 03… plus the FILETIME of the moment to disable. The Run value or the
// shortcut itself is never touched.
func setApproved(hive, sub, name string, enabled bool) error {
	h, err := createKey(hiveRoot[hive], approvedPath+sub)
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(h)
	data := make([]byte, 12)
	if enabled {
		data[0] = 2
	} else {
		data[0] = 3
		ft := syscall.NsecToFiletime(time.Now().UnixNano())
		binary.LittleEndian.PutUint32(data[4:], ft.LowDateTime)
		binary.LittleEndian.PutUint32(data[8:], ft.HighDateTime)
	}
	return regSet(h, name, syscall.REG_BINARY, data)
}

type runSource struct{ hive, path, approved, scope string }

var runSources = []runSource{
	{"HKCU", runPath, "Run", "Current user"},
	{"HKLM", runPath, "Run", "All users"},
	{"HKLM", run32Path, "Run32", "All users (32-bit)"},
}

// listApps reads the Run keys and both Startup folders.
func listApps(running map[string]bool) []Item {
	var items []Item
	for _, src := range runSources {
		for _, v := range readValues(hiveRoot[src.hive], src.path) {
			if v.name == "" || (v.typ != syscall.REG_SZ && v.typ != syscall.REG_EXPAND_SZ) {
				continue
			}
			cmd := strings.TrimSpace(utf16z(v.data))
			exe, _ := splitCommand(cmd)
			// An HKLM item can also be switched off for one user through
			// that user's own hive — honour both.
			off := approvedDisabled(src.hive, src.approved, v.name) || (src.hive == "HKLM" && approvedDisabled("HKCU", src.approved, v.name))
			full := src.hive + `\` + src.path
			items = append(items, Item{
				Kind: KindRun, Key: "run|" + src.hive + "|" + src.approved + "|" + v.name,
				Name: v.name, Command: cmd, Exe: exe, Location: full, reg: full, Scope: src.scope,
				Trigger: "At sign-in", Enabled: !off, Running: exe != "" && running[strings.ToLower(exe)],
				hive: src.hive, approved: src.approved, value: v.name,
			})
		}
	}
	folders := []struct{ hive, dir, scope string }{
		{"HKCU", filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Startup`), "Current user"},
		{"HKLM", filepath.Join(os.Getenv("ProgramData"), `Microsoft\Windows\Start Menu\Programs\Startup`), "All users"},
	}
	for _, f := range folders {
		ents, err := os.ReadDir(f.dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || strings.EqualFold(e.Name(), "desktop.ini") {
				continue
			}
			path := filepath.Join(f.dir, e.Name())
			target := path
			if strings.EqualFold(filepath.Ext(path), ".lnk") {
				if t := uninstall.ResolveShortcut(path); t != "" {
					target = t
				}
			}
			exe, _ := splitCommand(`"` + target + `"`)
			items = append(items, Item{
				Kind: KindFolder, Key: "folder|" + f.hive + "|" + e.Name(),
				Name: strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), Command: target, Exe: exe,
				Location: path, Scope: f.scope, Trigger: "At sign-in",
				Enabled: !approvedDisabled(f.hive, "StartupFolder", e.Name()) && !(f.hive == "HKLM" && approvedDisabled("HKCU", "StartupFolder", e.Name())),
				Running: exe != "" && running[strings.ToLower(exe)],
				hive:    f.hive, approved: "StartupFolder", value: e.Name(),
			})
		}
	}
	return items
}

// setAppEnabled flips an app item through StartupApproved. Re-enabling an
// all-users item also clears a per-user off switch, or "Enable" would
// appear to do nothing.
func setAppEnabled(it Item, on bool) error {
	if err := setApproved(it.hive, it.approved, it.value, on); err != nil {
		return fmt.Errorf("%s\\...\\StartupApproved\\%s: %w", it.hive, it.approved, err)
	}
	if on && it.hive == "HKLM" && approvedDisabled("HKCU", it.approved, it.value) {
		return setApproved("HKCU", it.approved, it.value, true)
	}
	return nil
}
