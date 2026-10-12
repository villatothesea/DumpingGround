//go:build windows

package disk

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	shFileOpDLL      = syscall.NewLazyDLL("shell32.dll")
	procSHFileOp     = shFileOpDLL.NewProc("SHFileOperationW")
	coInitDLL        = syscall.NewLazyDLL("ole32.dll")
	procCoInitEx     = coInitDLL.NewProc("CoInitializeEx")
	rcyKernel32      = syscall.NewLazyDLL("kernel32.dll")
	procGetDriveType = rcyKernel32.NewProc("GetDriveTypeW")
	procDiskFree     = rcyKernel32.NewProc("GetDiskFreeSpaceExW")
	rcyAdvapi        = syscall.NewLazyDLL("advapi32.dll")
	procRcyOpenKey   = rcyAdvapi.NewProc("RegOpenKeyExW")
	procRcyCloseKey  = rcyAdvapi.NewProc("RegCloseKey")
	procRcyQueryVal  = rcyAdvapi.NewProc("RegQueryValueExW")
)

// shFileOpStruct mirrors SHFILEOPSTRUCTW for FO_DELETE calls.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 0x3
	fofSilent         = 0x4
	fofNoConfirmation = 0x10
	fofAllowUndo      = 0x40
	fofNoErrorUI      = 0x400
)

// RecycleFileOrDir moves a file or directory to the recycle bin through
// the shell. Any condition under which the shell would nuke instead —
// an over-long path, a drive without a bin, a bin turned off in
// Explorer policy — is checked here first and returned as an error, so
// the recycle path never silently becomes a permanent delete.
func RecycleFileOrDir(path string) error {
	return RecycleFileOrDirSized(path, 0)
}

// RecycleFileOrDirSized additionally checks the item against the
// drive's bin capacity — an item bigger than the whole bin is deleted
// permanently even with FOF_ALLOWUNDO, so it is refused up front.
// bytes of 0 skips the capacity check.
func RecycleFileOrDirSized(path string, bytes uint64) error {
	if err := recyclable(path); err != nil {
		return err
	}
	if bytes > 0 {
		if cap, ok := binCapacity(path); ok && bytes > cap {
			return fmt.Errorf("too large for the recycle bin on %s — refusing to delete %q", filepath.VolumeName(path), path)
		}
	}
	// SHFileOperationW is a shell call that may create COM objects on the
	// calling thread; make sure the apartment exists.
	procCoInitEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED — S_FALSE is fine
	p, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	// pFrom expects a double-null-terminated list of paths.
	p = append(p, 0)
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &p[0],
		fFlags: fofAllowUndo | fofSilent | fofNoConfirmation | fofNoErrorUI,
	}
	r, _, _ := procSHFileOp.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return errors.New("recycle bin move refused by the shell")
	}
	if op.fAnyOperationsAborted != 0 {
		return errors.New("recycle bin move aborted")
	}
	return nil
}

// recyclable pre-checks a path so the recycle promise holds: only an
// absolute, wildcard-free, reasonably short path on a drive whose
// recycle bin exists and is enabled can actually be recycled —
// anything else SHFileOperation would delete permanently while still
// reporting success.
func recyclable(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("refusing relative path %q", path)
	}
	if strings.ContainsAny(path, "*?") {
		return fmt.Errorf("refusing wildcard path %q", path)
	}
	if len(path) >= 240 {
		return fmt.Errorf("path too long for the recycle bin (%d chars)", len(path))
	}
	vol := filepath.VolumeName(path)
	if len(vol) != 2 || vol[1] != ':' {
		return fmt.Errorf("no drive letter in %q — UNC paths have no recycle bin", path)
	}
	rp, err := syscall.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return err
	}
	t, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(rp)))
	if t != 3 && t != 2 { // DRIVE_FIXED, DRIVE_REMOVABLE only
		return fmt.Errorf("drive %s cannot hold a recycle bin", vol)
	}
	if nukeOnDelete(vol[0]) {
		return fmt.Errorf("the recycle bin is disabled on drive %s — refusing to delete %q", vol, path)
	}
	return nil
}

// binCapacity estimates the bin's maximum size on the path's drive:
// Explorer's BitBucket MaxCapacity is a percentage of the drive.
// Unknown settings report ok=false — the caller then skips the size
// check rather than guessing.
func binCapacity(path string) (uint64, bool) {
	vol := filepath.VolumeName(path)
	if len(vol) != 2 {
		return 0, false
	}
	pct, ok := bitbucketDWORD(vol[0], "MaxCapacity")
	if !ok {
		return 0, false
	}
	root, err := syscall.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return 0, false
	}
	var freeAvail, total, totalFree uint64
	r, _, _ := procDiskFree.Call(uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&freeAvail)), uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)))
	if r == 0 || total == 0 {
		return 0, false
	}
	return total * uint64(pct) / 100, true
}

// nukeOnDelete reports whether Explorer's BitBucket policy turns the
// bin off for this drive — deletions there bypass the bin entirely
// even with FOF_ALLOWUNDO.
func nukeOnDelete(drive byte) bool {
	v, ok := bitbucketDWORD(drive, "NukeOnDelete")
	return ok && v == 1
}

// bitbucketDWORD reads one DWORD under the drive's BitBucket policy
// key — NukeOnDelete and MaxCapacity live there.
func bitbucketDWORD(drive byte, name string) (uint32, bool) {
	sub := `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\BitBucket\` + string(drive)
	subP, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return 0, false
	}
	var h uintptr
	r, _, _ := procRcyOpenKey.Call(0x80000001, /*HKEY_CURRENT_USER*/
		uintptr(unsafe.Pointer(subP)), 0, 0x20019, /*KEY_READ*/
		uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, false
	}
	defer procRcyCloseKey.Call(h)
	nameP, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, false
	}
	var kind, val, sz uint32 = 0, 0, 4
	r, _, _ = procRcyQueryVal.Call(h, uintptr(unsafe.Pointer(nameP)), 0,
		uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&val)),
		uintptr(unsafe.Pointer(&sz)))
	return val, r == 0 && kind == 4
}
