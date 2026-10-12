//go:build windows

// Package elevwork is the --elevated-worker side of elevate: a hidden,
// short-lived elevated process that answers one named pipe belonging to
// its parent and nothing else. It is not a service — it exits when the
// parent exits, when the pipe dies, or after idle.
//
// Security boundary:
//   - one client ever, and only the PID named on the command line —
//     GetNamedPipeClientProcessId is kernel truth, it cannot be faked
//   - PIPE_REJECT_REMOTE_CLIENTS — no network reach
//   - a closed verb set — no shell, no arbitrary paths; registry and
//     filesystem writes pass the same guards the parent applies
//   - every operation is written to the app log
package elevwork

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/dumpingground/dumpingground/internal/applog"
	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/elevate"
)

var (
	k32 = syscall.NewLazyDLL("kernel32.dll")
	a32 = syscall.NewLazyDLL("advapi32.dll")

	procCreateNamedPipe   = k32.NewProc("CreateNamedPipeW")
	procConnectNamedPipe  = k32.NewProc("ConnectNamedPipe")
	procDisconnectPipe    = k32.NewProc("DisconnectNamedPipe")
	procClientPID         = k32.NewProc("GetNamedPipeClientProcessId")
	procOpenProcess       = k32.NewProc("OpenProcess")
	procWaitForObject     = k32.NewProc("WaitForSingleObject")
	procDuplicateHandle   = k32.NewProc("DuplicateHandle")
	procQueryProcessImage = k32.NewProc("QueryFullProcessImageNameW")
	procGetCurrentProcess = k32.NewProc("GetCurrentProcess")
	procRegOpen           = a32.NewProc("RegOpenKeyExW")
	procRegDeleteTree     = a32.NewProc("RegDeleteTreeW")
	procRegDeleteValue    = a32.NewProc("RegDeleteValueW")
	procRegClose          = a32.NewProc("RegCloseKey")
)

const (
	pipeAccessDuplex  = 0x3
	pipeTypeMessage   = 0x4
	pipeReadMessage   = 0x2
	pipeWait          = 0x0
	pipeRejectRemote  = 0x8
	pipeFirstInstance = 0x80000
	syncAccess        = 0x100000
	processDupHandle  = 0x40
	processQueryLimit = 0x1000
	dupSameAccess     = 0x2
	dupCloseSource    = 0x1
	keyWow64          = 0x100
	keyWow64_32       = 0x200
	maximumAllowed    = 0x02000000
	hkeyLocalMachine  = 0x80000002
	idleLimit         = 10 * time.Minute
)

var lastActivity atomic.Int64

// Serve runs the worker until the pipe dies, the parent dies, or the
// idle limit passes. Returns a process exit code.
func Serve(parentPID uint32, pipeName string) int {
	lastActivity.Store(time.Now().Unix())
	parent, err := openParent(parentPID)
	if err != nil {
		return 2
	}
	defer syscall.CloseHandle(parent)
	go func() {
		procWaitForObject.Call(uintptr(parent), ^uintptr(0)) // INFINITE
		applog.Logf("elevated worker: parent %d gone, exiting", parentPID)
		os.Exit(0)
	}()
	go idleWatch()

	p, err := syscall.UTF16PtrFromString(pipeName)
	if err != nil {
		return 2
	}
	h, _, e := procCreateNamedPipe.Call(
		uintptr(unsafe.Pointer(p)),
		pipeAccessDuplex|pipeFirstInstance,
		pipeTypeMessage|pipeReadMessage|pipeWait|pipeRejectRemote,
		1, 1<<20, 1<<20, 0, 0)
	if h == 0 || h == ^uintptr(0) {
		applog.Logf("elevated worker: CreateNamedPipe failed: %v", e)
		return 2
	}
	pipe := syscall.Handle(h)
	defer syscall.CloseHandle(pipe)
	applog.Logf("elevated worker: up for parent %d", parentPID)

	// One client ever, and only the parent's PID.
	for {
		r, _, e := procConnectNamedPipe.Call(uintptr(pipe), 0)
		// ERROR_PIPE_CONNECTED: the client was already waiting when we
		// got here — that is a successful connection, not a failure.
		if r == 0 && e != syscall.Errno(535) {
			applog.Logf("elevated worker: connect failed: %v", e)
			return 2
		}
		var clientPID uint32
		if r, _, _ := procClientPID.Call(uintptr(pipe), uintptr(unsafe.Pointer(&clientPID))); r != 0 && clientPID == parentPID && clientIsSelf(clientPID) {
			break
		}
		applog.Logf("elevated worker: rejected client pid %d (want %d)", clientPID, parentPID)
		procDisconnectPipe.Call(uintptr(pipe))
	}
	serve(pipe, parent)
	return 0
}

// clientIsSelf closes the PID-reuse window around pipe creation: the
// kernel-true PID matching the parent is not enough if the parent died
// and a same-user process recycled the number — the image path must be
// this very executable.
func clientIsSelf(pid uint32) bool {
	h, _, _ := procOpenProcess.Call(processQueryLimit, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	var buf [syscall.MAX_LONG_PATH]uint16
	n := uint32(len(buf))
	r, _, _ := procQueryProcessImage.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(syscall.UTF16ToString(buf[:n]), self)
}

func openParent(pid uint32) (syscall.Handle, error) {
	h, _, e := procOpenProcess.Call(syncAccess|processDupHandle|processQueryLimit, 0, uintptr(pid))
	if h == 0 {
		return 0, fmt.Errorf("open parent %d: %v", pid, e)
	}
	return syscall.Handle(h), nil
}

func serve(pipe syscall.Handle, parent syscall.Handle) {
	f := os.NewFile(uintptr(pipe), "elev-pipe")
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			return
		}
		n := binary.LittleEndian.Uint32(hdr[:])
		if n == 0 || n > 4<<20 {
			return
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(f, buf); err != nil {
			return
		}
		var req elevate.Request
		if err := json.Unmarshal(buf, &req); err != nil {
			return
		}
		lastActivity.Store(time.Now().Unix())
		resp := dispatch(&req, parent)
		out, _ := json.Marshal(resp)
		binary.LittleEndian.PutUint32(hdr[:], uint32(len(out)))
		if _, err := f.Write(hdr[:]); err != nil {
			return
		}
		if _, err := f.Write(out); err != nil {
			return
		}
		if req.Op == elevate.OpShutdown {
			return
		}
	}
}

func idleWatch() {
	for range time.Tick(30 * time.Second) {
		if time.Since(time.Unix(lastActivity.Load(), 0)) > idleLimit {
			applog.Logf("elevated worker: idle timeout, exiting")
			os.Exit(0)
		}
	}
}

func dispatch(req *elevate.Request, parent syscall.Handle) (r elevate.Response) {
	defer func() {
		if rec := recover(); rec != nil {
			r = elevate.Response{Err: fmt.Sprint("worker panic:", rec)}
		}
	}()
	switch req.Op {
	case elevate.OpPing:
		return elevate.Response{OK: true}
	case elevate.OpShutdown:
		applog.Logf("elevated worker: shutdown by parent")
		return elevate.Response{OK: true}
	case elevate.OpOpenVolume:
		return opOpenVolume(req, parent)
	case elevate.OpOpenProcess:
		return opOpenProcess(req, parent)
	case elevate.OpRegDelete:
		return opRegDelete(req)
	case elevate.OpDeletePath:
		return opDeletePath(req)
	case elevate.OpListKids:
		return opListKids(req)
	case elevate.OpListTree:
		return opListTree(req)
	}
	return elevate.Response{Err: "unknown op " + string(req.Op)}
}

// donate moves local into the parent's handle table; the access check
// already happened here, elevated, so the duplicated handle keeps it.
func donate(local syscall.Handle, parent syscall.Handle) (uint64, error) {
	self, _, _ := procGetCurrentProcess.Call()
	var out syscall.Handle
	r, _, e := procDuplicateHandle.Call(self, uintptr(local), uintptr(parent),
		uintptr(unsafe.Pointer(&out)), 0, 1 /*inherit=false*/, dupSameAccess|dupCloseSource)
	if r == 0 {
		return 0, e
	}
	return uint64(out), nil
}

func opOpenVolume(req *elevate.Request, parent syscall.Handle) elevate.Response {
	d := strings.ToUpper(strings.TrimSpace(req.Drive))
	if len(d) != 2 || d[1] != ':' || d[0] < 'A' || d[0] > 'Z' {
		return elevate.Response{Err: "bad drive " + req.Drive}
	}
	name, err := syscall.UTF16PtrFromString(`\\.\` + d)
	if err != nil {
		return elevate.Response{Err: err.Error()}
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return elevate.Response{Err: err.Error()}
	}
	out, err := donate(h, parent)
	if err != nil {
		syscall.CloseHandle(h)
		return elevate.Response{Err: err.Error()}
	}
	applog.Logf("elevated worker: opened volume %s", d)
	return elevate.Response{OK: true, Handle: out}
}

func opOpenProcess(req *elevate.Request, parent syscall.Handle) elevate.Response {
	const allowed = 0x1 | 0x10 | 0x40 | 0x400 | 0x800 | 0x1000 // terminate|vmread|dup|queryinfo|vmquery|qlimit
	if req.PID == 0 || req.Access == 0 || req.Access&^allowed != 0 {
		return elevate.Response{Err: "refused access mask"}
	}
	h, _, e := procOpenProcess.Call(uintptr(req.Access), 0, uintptr(req.PID))
	if h == 0 {
		return elevate.Response{Err: e.Error()}
	}
	out, err := donate(syscall.Handle(h), parent)
	if err != nil {
		syscall.CloseHandle(syscall.Handle(h))
		return elevate.Response{Err: err.Error()}
	}
	applog.Logf("elevated worker: opened pid %d mask %#x", req.PID, req.Access)
	return elevate.Response{OK: true, Handle: out}
}

// Deliberately absent: SeDebugPrivilege. Everything it unlocks here —
// reading a protected process's memory or handle table, terminating
// csrss/lsass — is debugger territory, and a VM_READ handle on lsass is
// the textbook credential-dump primitive. The kernel draws the line
// for us; a plain OpenProcess refusal is the answer, not a retry.

// regAllowed gates HKLM deletes to leftover-shaped keys: inside the
// uninstall hives, startup and App Paths lists, plus SOFTWARE\<vendor>
// trees — minus anything under Microsoft\Windows* outside those lists.
// valueOp means the request deletes a single value; only then may the
// target BE a list root (a Run value lives directly on it).
func regAllowed(key string, valueOp bool) bool {
	k := strings.ToUpper(strings.Trim(strings.ReplaceAll(key, "/", `\`), `\`))
	allow := []string{
		`SOFTWARE\MICROSOFT\WINDOWS\CURRENTVERSION\UNINSTALL`,
		`SOFTWARE\WOW6432NODE\MICROSOFT\WINDOWS\CURRENTVERSION\UNINSTALL`,
		`SOFTWARE\MICROSOFT\WINDOWS\CURRENTVERSION\RUN`,
		`SOFTWARE\MICROSOFT\WINDOWS\CURRENTVERSION\RUNONCE`,
		`SOFTWARE\MICROSOFT\WINDOWS\CURRENTVERSION\RUNONCEEX`,
		`SOFTWARE\MICROSOFT\WINDOWS\CURRENTVERSION\APP PATHS`,
		`SOFTWARE\MICROSOFT\WINDOWS\CURRENTVERSION\EXPLORER\STARTUPAPPROVED`,
	}
	for _, p := range allow {
		if k == p {
			return valueOp // the list root itself holds deletable values, not a deletable key
		}
		if strings.HasPrefix(k, p+`\`) {
			return true
		}
	}
	deny := []string{`SOFTWARE\MICROSOFT`, `SOFTWARE\CLASSES`, `SOFTWARE\POLICIES`, `SOFTWARE\WOW6432NODE\MICROSOFT`}
	for _, p := range deny {
		if k == p || strings.HasPrefix(k, p+`\`) {
			return false
		}
	}
	return strings.HasPrefix(k, `SOFTWARE\`) && len(k) > len(`SOFTWARE\`)
}

func opRegDelete(req *elevate.Request) elevate.Response {
	if !regAllowed(req.Key, req.Value != "") {
		return elevate.Response{Err: "refused registry path " + req.Key}
	}
	sub, err := syscall.UTF16PtrFromString(req.Key)
	if err != nil {
		return elevate.Response{Err: err.Error()}
	}
	access := uintptr(maximumAllowed | keyWow64)
	if req.Reg32 {
		access = maximumAllowed | keyWow64_32
	}
	var k uintptr
	r, _, e := procRegOpen.Call(hkeyLocalMachine, uintptr(unsafe.Pointer(sub)), 0, access, uintptr(unsafe.Pointer(&k)))
	if r != 0 {
		return elevate.Response{Err: e.Error()}
	}
	defer procRegClose.Call(k)
	if req.Value == "" {
		r, _, e = procRegDeleteTree.Call(k, 0)
	} else {
		val, err := syscall.UTF16PtrFromString(req.Value)
		if err != nil {
			return elevate.Response{Err: err.Error()}
		}
		r, _, e = procRegDeleteValue.Call(k, uintptr(unsafe.Pointer(val)))
	}
	if r != 0 {
		return elevate.Response{Err: e.Error()}
	}
	applog.Logf("elevated worker: reg delete HKLM\\%s value=%q", req.Key, req.Value)
	return elevate.Response{OK: true}
}

// safeTarget is the worker's filesystem guard — deletion must stay a
// delete of something plausible as a leftover or junk item, never a
// system root or a whole profile. The parent already ran its own
// checks; this is the second gate at the privilege boundary.
func safeTarget(path string) bool {
	if path == "" || !filepath.IsAbs(path) || strings.ContainsAny(path, "*?<>|\"") {
		return false
	}
	vol := filepath.VolumeName(path)
	if len(vol) != 2 || vol[1] != ':' {
		return false
	}
	rel := strings.Trim(strings.TrimPrefix(path, vol), `\`)
	if rel == "" {
		return false // drive root
	}
	segs := strings.Split(rel, `\`)
	switch strings.ToUpper(segs[0]) {
	case "WINDOWS":
		if len(segs) == 1 {
			return false
		}
		for _, core := range []string{"SYSTEM32", "SYSWOW64", "WINSXS", "SERVICING", "BOOT", "SYSTEMAPPS", "FONTS", "INF"} {
			if strings.EqualFold(segs[1], core) {
				return false
			}
		}
		return true
	case "USERS":
		if len(segs) <= 2 {
			return false // Users root or a whole profile
		}
		for _, std := range []string{"DESKTOP", "DOCUMENTS", "DOWNLOADS", "PICTURES", "MUSIC", "VIDEOS", "SAVED GAMES", "CONTACTS", "SEARCHES"} {
			if len(segs) == 3 && strings.EqualFold(segs[2], std) {
				return false
			}
		}
		return true
	case "PROGRAM FILES", "PROGRAM FILES (X86)", "PROGRAMDATA", "$RECYCLE.BIN":
		return len(segs) > 1
	case "PERFLOGS", "INTEL", "AMD":
		return false
	}
	return true
}

// anyReparse walks every component of path — RemoveAll and the shell
// both traverse an intermediate junction into its target, so a leaf
// that is not itself a reparse point is not enough.
func anyReparse(path string) bool {
	vol := filepath.VolumeName(path)
	rel := strings.TrimPrefix(path, vol)
	cur := vol
	for _, seg := range strings.FieldsFunc(rel, func(r rune) bool { return r == '\\' || r == '/' }) {
		cur = cur + `\` + seg
		fi, err := os.Lstat(cur)
		if err != nil {
			continue
		}
		if w32, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok && w32.FileAttributes&0x400 != 0 {
			return true
		}
	}
	return false
}

func opDeletePath(req *elevate.Request) elevate.Response {
	if !safeTarget(req.Path) || anyReparse(req.Path) {
		return elevate.Response{Err: "refused path " + req.Path}
	}
	var err error
	if req.Recycle {
		err = disk.RecycleFileOrDir(req.Path)
	} else {
		err = os.RemoveAll(req.Path)
	}
	if err != nil {
		return elevate.Response{Err: err.Error()}
	}
	applog.Logf("elevated worker: delete %s recycle=%v", req.Path, req.Recycle)
	return elevate.Response{OK: true}
}

// filetimeUnix converts a Windows FILETIME to unix seconds.
func filetimeUnix(ft syscall.Filetime) int64 {
	return (int64(ft.HighDateTime)<<32|int64(ft.LowDateTime))/1e7 - 11644473600
}

func opListKids(req *elevate.Request) elevate.Response {
	if req.Path == "" || !filepath.IsAbs(req.Path) {
		return elevate.Response{Err: "bad path"}
	}
	ents, err := os.ReadDir(req.Path)
	if err != nil {
		return elevate.Response{Err: err.Error()}
	}
	kids := make([]elevate.Kid, 0, len(ents))
	for _, e := range ents {
		k := elevate.Kid{Name: e.Name(), Dir: e.IsDir()}
		fillKid(&k, e)
		kids = append(kids, k)
	}
	return elevate.Response{OK: true, Kids: kids}
}

func fillKid(k *elevate.Kid, e os.DirEntry) {
	fi, err := e.Info()
	if err != nil {
		return
	}
	k.Size, k.ModUnix = fi.Size(), fi.ModTime().Unix()
	if w32, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		k.CrtUnix = filetimeUnix(w32.CreationTime)
		k.AccUnix = filetimeUnix(w32.LastAccessTime)
		k.Reparse = w32.FileAttributes&0x400 != 0
	}
}

// opListTree walks a whole subtree in one shot — protected system dirs
// deny even READ to the parent, so enumerating level by level through
// the pipe would be one RPC per directory.
func opListTree(req *elevate.Request) elevate.Response {
	if req.Path == "" || !filepath.IsAbs(req.Path) {
		return elevate.Response{Err: "bad path"}
	}
	root := filepath.Clean(req.Path)
	kids := make([]elevate.Kid, 0, 512)
	// A 25k-entry listing stays well under the client's 4MB frame cap —
	// a bigger protected dir is refused, not silently truncated.
	const cap = 25_000
	var big bool
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		if len(kids) >= cap {
			big = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		k := elevate.Kid{Name: rel, Dir: e.IsDir()}
		fillKid(&k, e)
		kids = append(kids, k)
		if k.Reparse && e.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if big {
		return elevate.Response{Err: "listing over 25k entries"}
	}
	if err != nil && len(kids) == 0 {
		return elevate.Response{Err: err.Error()}
	}
	return elevate.Response{OK: true, Kids: kids}
}
