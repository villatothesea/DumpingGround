//go:build windows

package uninstall

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/elevate"
)

var errNoRegBackup = errors.New("no .reg backup — registry item kept for safety")

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
// carries one error (or nil) per item, in order. progress, when given,
// hears (done, total, currentPath) before each item and once at the
// end — the review dialog's progress bar.
func RemoveItems(items []Item, backup io.Writer, progress ...func(done, total int, path string)) []error {
	rep := func(i int, path string) {
		if len(progress) > 0 && progress[0] != nil {
			progress[0](i, len(items), path)
		}
	}
	errs := make([]error, len(items))
	var bw *regBackupWriter
	if backup != nil {
		bw = newRegBackupWriter(backup)
		bw.WriteString("Windows Registry Editor Version 5.00\r\n\r\n")
	}
	for i, it := range items {
		rep(i, it.Path)
		switch it.Kind {
		case KindDir:
			errs[i] = disk.RecycleFileOrDirSized(it.Path, it.Bytes)
			// Protected leftovers (Program Files, system dirs) refuse
			// the plain delete — retry through the elevated worker.
			if errs[i] != nil && elevate.IsAccessDenied(errs[i]) {
				if err := elevate.DeletePath(it.Path, true); err == nil {
					errs[i] = nil
				}
			}
		case KindRegKey:
			if bw == nil {
				errs[i] = errNoRegBackup
				continue
			}
			root, sub, access, err := splitRegPath(it.Path, it.Reg32)
			if err != nil {
				errs[i] = err
				continue
			}
			if err := exportRegKey(bw, root, keyRead|access&keyWow64_32, sub, regDisplayRoot(it.Path)); err != nil {
				errs[i] = fmt.Errorf("backup failed, key kept: %w", err)
				continue
			}
			errs[i] = deleteRegTree(root, sub, access)
			if errs[i] != nil && root == hkeyLocalMachine && elevate.IsAccessDenied(errs[i]) {
				if err := elevate.RegDelete(sub, "", it.Reg32); err == nil {
					errs[i] = nil
				}
			}
		case KindRegValue:
			if bw == nil {
				errs[i] = errNoRegBackup
				continue
			}
			root, sub, access, err := splitRegPath(it.Path, it.Reg32)
			if err != nil {
				errs[i] = err
				continue
			}
			if err := exportRegValue(bw, root, keyRead|access&keyWow64_32, sub, it.RegValue, regDisplayRoot(it.Path)); err != nil {
				errs[i] = fmt.Errorf("backup failed, value kept: %w", err)
				continue
			}
			errs[i] = deleteRegValue(root, sub, it.RegValue, access)
			if errs[i] != nil && root == hkeyLocalMachine && elevate.IsAccessDenied(errs[i]) {
				if err := elevate.RegDelete(sub, it.RegValue, it.Reg32); err == nil {
					errs[i] = nil
				}
			}
		}
	}
	rep(len(items), "")
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

// regBackupWriter encodes the backup as UTF-16LE — regedit refuses a
// "Version 5.00" file that isn't, so UTF-8 output would be a backup in
// name only. The BOM goes first; write errors are latched so the
// exporter can fail the deletion instead of claiming a backup.
type regBackupWriter struct {
	w   io.Writer
	err error
}

func newRegBackupWriter(w io.Writer) *regBackupWriter {
	bw := &regBackupWriter{w: w}
	bw.WriteString("\ufeff")
	return bw
}

func (rw *regBackupWriter) WriteString(s string) {
	if rw.err != nil {
		return
	}
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	_, rw.err = rw.w.Write(b)
}

// exportRegKey writes a key, its values and every descendant to the
// .reg backup — the copy taken before removal.
func exportRegKey(w *regBackupWriter, root, access uintptr, sub, display string) error {
	k, err := openKey(root, access, sub)
	if err != nil {
		return err
	}
	defer k.close()
	w.WriteString("[" + display + "]\r\n")
	vals, _ := k.values()
	names := make([]string, 0, len(vals))
	for n := range vals {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w.WriteString(regLine(n, vals[n]))
	}
	w.WriteString("\r\n")
	for _, child := range mustSubKeys(k) {
		if err := exportRegKey(w, root, access, sub+`\`+child, display+`\`+child); err != nil {
			return err
		}
	}
	return w.err
}

// exportRegValue writes a single value's key section to the backup.
func exportRegValue(w *regBackupWriter, root, access uintptr, sub, value, display string) error {
	k, err := openKey(root, access, sub)
	if err != nil {
		return err
	}
	defer k.close()
	w.WriteString("[" + display + "]\r\n")
	if vals, err := k.values(); err == nil {
		if v, ok := vals[value]; ok {
			w.WriteString(regLine(value, v))
		}
	}
	w.WriteString("\r\n")
	return w.err
}

// regLine formats one value as a .reg line, preserving its type —
// an EXPAND_SZ written as plain "..." restores as REG_SZ, silently
// breaking paths holding %VAR%.
func regLine(name string, v regValue) string {
	field := `"`
	if name == "" {
		field = "@"
	} else {
		field += strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	switch v.kind {
	case regSz:
		s := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v.str())
		return field + `="` + s + `"` + "\r\n"
	case regExpandSz:
		return field + "=hex(2):" + hexBytes(v.data) + "\r\n"
	case regDword:
		return fmt.Sprintf("%s=dword:%08x\r\n", field, uint32(v.dword()))
	case regMultiSz:
		return field + "=hex(7):" + hexBytes(v.data) + "\r\n"
	case regQword:
		return field + "=hex(b):" + hexBytes(v.data) + "\r\n"
	default:
		return field + "=hex:" + hexBytes(v.data) + "\r\n"
	}
}

func hexBytes(b []byte) string {
	var sb strings.Builder
	for i, x := range b {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%02x", x)
	}
	return sb.String()
}
