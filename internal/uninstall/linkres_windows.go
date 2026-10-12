//go:build windows

package uninstall

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// COM resolve of .lnk targets. LinkInfo's recorded path is only a
// hint — Windows resolves clicks through the link's PIDL plus
// distributed link tracking, so a shortcut whose stored path is stale
// can still open fine (Magpie: LinkInfo named the deleted installer in
// a temp downloads folder, while the PIDL tracks the moved app). Only
// IShellLink::Resolve answers "is this shortcut dead" honestly.

var ole32 = syscall.NewLazyDLL("ole32.dll")

var (
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
)

var (
	clsidShellLink  = syscall.GUID{0x00021401, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW  = syscall.GUID{0x000214F9, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = syscall.GUID{0x0000010B, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// comVtable calls the COM method at index on object ppv.
func comVtable(ppv uintptr, index uintptr, args ...uintptr) uintptr {
	vt := *(*uintptr)(unsafe.Pointer(ppv))
	fn := *(*uintptr)(unsafe.Pointer(vt + index*unsafe.Sizeof(uintptr(0))))
	all := append([]uintptr{ppv}, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func comRelease(ppv uintptr) { comVtable(ppv, 2) }

// linkResolver holds one COM-initialized thread for a batch of link
// checks — CoInitialize once per scan, not once per file.
type linkResolver struct {
	shell uintptr // IShellLinkW*
	pers  uintptr // IPersistFile*
	live  bool
}

func newLinkResolver() *linkResolver {
	r := &linkResolver{}
	// COM calls must run on the initializing thread.
	runtime.LockOSThread()
	r.live = true
	hr, _, _ := procCoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
	if hr != 0 && hr != 1 {                   // S_OK / S_FALSE both fine
		return r
	}
	if hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, 1,
		uintptr(unsafe.Pointer(&iidIShellLinkW)),
		uintptr(unsafe.Pointer(&r.shell))); hr != 0 || r.shell == 0 {
		return r
	}
	// QueryInterface for IPersistFile — method 0.
	if comVtable(r.shell, 0, uintptr(unsafe.Pointer(&iidIPersistFile)),
		uintptr(unsafe.Pointer(&r.pers))) != 0 || r.pers == 0 {
		comRelease(r.shell)
		r.shell = 0
	}
	return r
}

func (r *linkResolver) close() {
	if r.pers != 0 {
		comRelease(r.pers)
	}
	if r.shell != 0 {
		comRelease(r.shell)
	}
	if r.live {
		procCoUninitialize.Call()
		runtime.UnlockOSThread()
	}
	r.live = false
}

// resolve reports what a click on the link would reach. resolved=true
// means Windows itself found the target — the strongest possible "not
// dead" signal. inconclusive means the check could not run at all.
func (r *linkResolver) resolve(path string) (target string, resolved, inconclusive bool) {
	if r.shell == 0 || r.pers == 0 {
		return "", false, true
	}
	up, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", false, true
	}
	// IPersistFile::Load (index 5), STGM_READ.
	if comVtable(r.pers, 5, uintptr(unsafe.Pointer(up)), 0) != 0 {
		return "", false, true
	}
	// IShellLinkW::Resolve (index 19): SLR_NO_UI|SLR_NOUPDATE — search
	// silently, do not rewrite the link.
	if comVtable(r.shell, 19, 0, 0x1|0x8) != 0 {
		return "", false, false // resolve ran and failed — dead
	}
	// IShellLinkW::GetPath (index 3), SLGP_RAWPATH.
	buf := make([]uint16, 4096)
	if comVtable(r.shell, 3, uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)), 0, 0x4) != 0 {
		return "", true, false // resolved but path unreadable — treat as live
	}
	return syscall.UTF16ToString(buf), true, false
}

// linkDead decides whether a shortcut is a leftover: the shell's own
// resolution machinery cannot find what it opens. A successful Resolve
// means the link works — including shortcuts to shell namespace
// objects (Control Panel applets, File Explorer) which have no file
// path at all, so no second-guessing the resolved path's existence.
// An inconclusive check flags nothing — flagging on a failed resolve
// would condemn working links.
func (r *linkResolver) linkDead(path string) bool {
	// The cheap check first: the stored target exists, or cannot be
	// proven gone (offline drive, unreachable share) — either way the
	// link is not condemned on doubt.
	if t := parseLinkTargetFile(path); t != "" && missingPath(t) == "" {
		return false
	}
	_, resolved, inconclusive := r.resolve(path)
	return !inconclusive && !resolved
}

// parseLinkTargetFile reads the lnk and returns its LinkInfo target.
func parseLinkTargetFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return ""
	}
	return parseLinkTarget(data)
}

// ResolveShortcut returns the raw target a .lnk points at ("" when the
// shell cannot resolve it). The startup page uses it to show what a
// Startup-folder shortcut actually launches.
func ResolveShortcut(path string) string {
	r := newLinkResolver()
	defer r.close()
	target, _, _ := r.resolve(path)
	return target
}
