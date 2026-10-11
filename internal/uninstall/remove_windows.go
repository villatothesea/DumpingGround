//go:build windows

package uninstall

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/dumpingground/dumpingground/internal/disk"
)

var (
	procRegDeleteTree  = advapi.NewProc("RegDeleteTreeW")
	procRegDeleteValue = advapi.NewProc("RegDeleteValueW")
)

const (
	keySetValue = 0x20002 // KEY_SET_VALUE (STANDARD_RIGHTS_WRITE | KEY_SET_VALUE)
	keyWriteReg = 0x20006 // KEY_WRITE
	keyWow64_32 = 0x0100  // same view-select bit as keyWow64 in registry_windows.go
)

// RemoveItems deletes leftover candidates reversibly: directories go
// to the Recycle Bin; registry items are first exported to backup in
// .reg format so a double-click restores them. The returned slice
// carries one error (or nil) per item, in order.
func RemoveItems(items []Item, backup io.Writer) []error {
	errs := make([]error, len(items))
	if backup != nil {
		io.WriteString(backup, "Windows Registry Editor Version 5.00\r\n\r\n")
	}
	for i, it := range items {
		switch it.Kind {
		case KindDir:
			errs[i] = disk.RecycleFileOrDir(it.Path)
		case KindRegKey:
			root, sub, access, err := splitRegPath(it.Path, it.Reg32)
			if err != nil {
				errs[i] = err
				continue
			}
			if backup != nil {
				_ = exportRegKey(backup, root, keyRead|access&keyWow64_32, sub, regDisplayRoot(it.Path))
			}
			errs[i] = deleteRegTree(root, sub, access)
		case KindRegValue:
			root, sub, access, err := splitRegPath(it.Path, it.Reg32)
			if err != nil {
				errs[i] = err
				continue
			}
			if backup != nil {
				_ = exportRegValue(backup, root, keyRead|access&keyWow64_32, sub, it.RegValue, regDisplayRoot(it.Path))
			}
			errs[i] = deleteRegValue(root, sub, it.RegValue, access)
		}
	}
	return errs
}

// regDisplayRoot converts our "HKLM"/"HKCU" prefix into the canonical
// .reg hive name.
func regDisplayRoot(path string) string {
	if strings.HasPrefix(path, "HKLM\\") {
		return "HKEY_LOCAL_MACHINE" + path[4:]
	}
	return "HKEY_CURRENT_USER" + path[4:]
}

// splitRegPath parses an item path into hive handle, subkey path and
// the access mask (with KEY_WOW64_32KEY when the entry is 32-bit).
func splitRegPath(path string, wow32 bool) (root uintptr, sub string, access uintptr, err error) {
	switch {
	case strings.HasPrefix(path, "HKLM\\"):
		root, sub = hkeyLocalMachine, path[5:]
	case strings.HasPrefix(path, "HKCU\\"):
		root, sub = hkeyCurrentUser, path[5:]
	default:
		return 0, "", 0, fmt.Errorf("unknown hive in %q", path)
	}
	access = keyWriteReg
	if wow32 {
		access |= keyWow64_32
	}
	return root, sub, access, nil
}

// deleteRegTree removes a key and everything under it. The parent key
// is opened for writing, then RegDeleteTree removes the named child.
func deleteRegTree(root uintptr, sub string, access uintptr) error {
	parent, name := splitSub(sub)
	k, err := openKey(root, access, parent)
	if err != nil {
		return err
	}
	defer k.close()
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r, _, _ := procRegDeleteTree.Call(uintptr(k), uintptr(unsafe.Pointer(p)))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// deleteRegValue removes one value from a key.
func deleteRegValue(root uintptr, sub, value string, access uintptr) error {
	k, err := openKey(root, keySetValue|access&keyWow64_32, sub)
	if err != nil {
		return err
	}
	defer k.close()
	p, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	r, _, _ := procRegDeleteValue.Call(uintptr(k), uintptr(unsafe.Pointer(p)))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// splitSub breaks "A\B\C" into parent "A\B" and leaf "C".
func splitSub(sub string) (string, string) {
	if i := strings.LastIndexByte(sub, '\\'); i >= 0 {
		return sub[:i], sub[i+1:]
	}
	return "", sub
}

// exportRegKey writes a key, its values and every descendant to a .reg
// stream — the backup taken before removal.
func exportRegKey(w io.Writer, root, access uintptr, sub, display string) error {
	k, err := openKey(root, access, sub)
	if err != nil {
		return err
	}
	defer k.close()
	io.WriteString(w, "["+display+"]\r\n")
	vals, _ := k.values()
	names := make([]string, 0, len(vals))
	for n := range vals {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		io.WriteString(w, regLine(n, vals[n]))
	}
	io.WriteString(w, "\r\n")
	for _, child := range mustSubKeys(k) {
		_ = exportRegKey(w, root, access, sub+`\`+child, display+`\`+child)
	}
	return nil
}

// exportRegValue writes a single value's key section to the backup.
func exportRegValue(w io.Writer, root, access uintptr, sub, value, display string) error {
	k, err := openKey(root, access, sub)
	if err != nil {
		return err
	}
	defer k.close()
	io.WriteString(w, "["+display+"]\r\n")
	if vals, err := k.values(); err == nil {
		if v, ok := vals[value]; ok {
			io.WriteString(w, regLine(value, v))
		}
	}
	io.WriteString(w, "\r\n")
	return nil
}

// regLine formats one value as a .reg line.
func regLine(name string, v regValue) string {
	field := `"`
	if name == "" {
		field = "@"
	} else {
		field += strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	switch v.kind {
	case regSz, regExpandSz:
		s := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v.str())
		return field + `="` + s + `"` + "\r\n"
	case regDword:
		return fmt.Sprintf("%s=dword:%08x\r\n", field, uint32(v.dword()))
	default:
		var hexs []string
		for _, b := range v.data {
			hexs = append(hexs, fmt.Sprintf("%02x", b))
		}
		return field + "=hex:" + strings.Join(hexs, ",") + "\r\n"
	}
}
