//go:build windows

package elevate

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32Elev = syscall.NewLazyDLL("kernel32.dll")
	shell32Elev  = syscall.NewLazyDLL("shell32.dll")
	advapiElev   = syscall.NewLazyDLL("advapi32.dll")

	procWaitNamedPipe    = kernel32Elev.NewProc("WaitNamedPipeW")
	procShellExecuteEx   = shell32Elev.NewProc("ShellExecuteExW")
	procOpenProcessToken = advapiElev.NewProc("OpenProcessToken")
	procGetTokenInfo     = advapiElev.NewProc("GetTokenInformation")
)

const (
	seeNoCloseProcess = 0x40
	errCancelled      = 1223 // ERROR_CANCELLED — the UAC "No"
)

var (
	mu  sync.Mutex // serializes every frame on the pipe
	cli *client
)

type client struct {
	f *os.File
}

// SelfElevated reports whether this process already runs elevated — when
// it does the worker never gets spawned because the try-first path just
// succeeds.
func SelfElevated() bool {
	var tok syscall.Handle
	r, _, _ := procOpenProcessToken.Call(uintptr(^uintptr(0)), 0x8, uintptr(unsafe.Pointer(&tok))) // GetCurrentProcess(), TOKEN_QUERY
	if r == 0 {
		return false
	}
	defer syscall.CloseHandle(tok)
	var elevated, ret uint32
	r, _, _ = procGetTokenInfo.Call(uintptr(tok), 20 /*TokenElevation*/, uintptr(unsafe.Pointer(&elevated)), 4, uintptr(unsafe.Pointer(&ret)))
	return r != 0 && elevated != 0
}

// Ensure connects to the worker, spawning it through a runas prompt the
// first time. Safe to call from any goroutine; one spawn at a time, and
// callers can be slow — the UAC consent prompt blocks ShellExecuteEx.
func Ensure() error {
	mu.Lock()
	defer mu.Unlock()
	if cli != nil {
		if r, err := cli.roundTrip(&Request{Op: OpPing}); err == nil && r.OK {
			return nil
		}
		cli.close()
		cli = nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var nonce [8]byte
	rand.Read(nonce[:])
	pipe := fmt.Sprintf(`\\.\pipe\DumpingGround-elev-%d-%s`, os.Getpid(), hex.EncodeToString(nonce[:]))
	args := fmt.Sprintf("--elevated-worker --elev-parent=%d --elev-pipe=%s", os.Getpid(), pipe)
	if err := shellRunAs(exe, args); err != nil {
		return err
	}
	// The prompt already resolved by the time ShellExecuteEx returns;
	// the worker just needs a moment to create the pipe.
	deadline := time.Now().Add(15 * time.Second)
	for {
		name, err := syscall.UTF16PtrFromString(pipe)
		if err != nil {
			return err
		}
		h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
		if err == nil {
			f := os.NewFile(uintptr(h), pipe)
			c := &client{f: f}
			if r, err := c.roundTrip(&Request{Op: OpPing}); err == nil && r.OK {
				cli = c
				return nil
			}
			f.Close()
			return fmt.Errorf("elevated worker handshake failed")
		}
		var en syscall.Errno
		switch {
		case errors.As(err, &en) && en == 231: // ERROR_PIPE_BUSY
			procWaitNamedPipe.Call(uintptr(unsafe.Pointer(name)), 2000)
		case errors.Is(err, syscall.ERROR_FILE_NOT_FOUND):
		default:
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("elevated worker did not come up")
		}
		time.Sleep(120 * time.Millisecond)
	}
}

func (c *client) close() { c.f.Close() }

// roundTrip is one frame each way. A transport error means the worker
// is gone — the caller drops cli so the next call respawns it.
func (c *client) roundTrip(req *Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := c.f.Write(hdr[:]); err != nil {
		return nil, err
	}
	if _, err := c.f.Write(body); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(c.f, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n == 0 || n > 4<<20 {
		return nil, fmt.Errorf("worker reply too large")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.f, buf); err != nil {
		return nil, err
	}
	var resp Response
	if err := json.Unmarshal(buf, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// call issues one request and returns the response; a broken pipe drops
// the worker so the next call respawns it.
func call(req *Request) (*Response, error) {
	if err := Ensure(); err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	resp, err := cli.roundTrip(req)
	if err != nil {
		cli.close()
		cli = nil
		return nil, err
	}
	if !resp.OK {
		return nil, errors.New(resp.Err)
	}
	return resp, nil
}

// OpenVolume returns a handle to \\.\<drive> valid in this process —
// opened by the worker, donated through DuplicateHandle. MFT scans and
// the USN watcher use it unchanged. uintptr keeps the signature
// compilable off Windows.
func OpenVolume(drive string) (uintptr, error) {
	r, err := call(&Request{Op: OpOpenVolume, Drive: drive})
	if err != nil {
		return 0, err
	}
	return uintptr(r.Handle), nil
}

// OpenProcessHandle donates an OpenProcess handle for the given access
// mask — the access check ran in the elevated worker, so the duplicated
// handle keeps those rights here.
func OpenProcessHandle(pid uint32, access uint32) (uintptr, error) {
	r, err := call(&Request{Op: OpOpenProcess, PID: pid, Access: access})
	if err != nil {
		return 0, err
	}
	return uintptr(r.Handle), nil
}

// RegDelete removes an HKLM key tree (Value empty) or a single value;
// wow32 selects the 32-bit registry view. The worker whitelists
// leftover-shaped paths; anything else is refused.
func RegDelete(key, value string, wow32 bool) error {
	_, err := call(&Request{Op: OpRegDelete, Key: key, Value: value, Reg32: wow32})
	return err
}

// DeletePath removes a protected path. Recycle goes through the worker's
// own SHFileOperation — same user, same bin.
func DeletePath(path string, recycle bool) error {
	_, err := call(&Request{Op: OpDeletePath, Path: path, Recycle: recycle})
	return err
}

// ListKids enumerates a directory the parent cannot read.
func ListKids(path string) ([]Kid, error) {
	r, err := call(&Request{Op: OpListKids, Path: path})
	if err != nil {
		return nil, err
	}
	return r.Kids, nil
}

// ListTree enumerates a whole subtree in one round-trip — Kid.Name is
// the root-relative path.
func ListTree(path string) ([]Kid, error) {
	r, err := call(&Request{Op: OpListTree, Path: path})
	if err != nil {
		return nil, err
	}
	return r.Kids, nil
}

// Active reports whether the elevated worker is currently connected —
// the UI shows a badge so privilege use stays visible.
func Active() bool {
	mu.Lock()
	defer mu.Unlock()
	return cli != nil
}

// Shutdown ends the worker politely; a dead parent would kill it anyway.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if cli != nil {
		_, _ = cli.roundTrip(&Request{Op: OpShutdown})
		cli.close()
		cli = nil
	}
}

// shellRunAs spawns our own exe elevated via the shell's runas verb.
// The call blocks inside the UAC consent UI — always call off the UI
// thread. Error 1223 means the user pressed No.
func shellRunAs(exe, args string) error {
	type shellExecInfo struct {
		cbSize, fMask   uint32
		hwnd            uintptr
		lpVerb, lpFile  *uint16
		lpParameters    *uint16
		lpDirectory     *uint16
		nShow           int32
		hInstApp        uintptr
		lpIDList        uintptr
		lpClass         *uint16
		hkeyClass       uintptr
		dwHotKey        uint32
		hIcon, hProcess uintptr
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	par, err := syscall.UTF16PtrFromString(args)
	if err != nil {
		return err
	}
	var info shellExecInfo
	info.cbSize = uint32(unsafe.Sizeof(info))
	info.fMask = seeNoCloseProcess
	info.lpVerb, info.lpFile, info.lpParameters = verb, file, par
	info.nShow = 0 // SW_HIDE — the worker owns no window
	r, _, e := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == errCancelled {
			return ErrCancelled
		}
		return e
	}
	if info.hProcess != 0 {
		syscall.CloseHandle(syscall.Handle(info.hProcess))
	}
	return nil
}

// IsAccessDenied reports whether err is a Windows access refusal — the
// trigger for a try-first → elevate retry.
func IsAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, syscall.EACCES) {
		return true
	}
	var en syscall.Errno
	if errors.As(err, &en) && (en == 5 || en == 100) { // ACCESS_DENIED / WSAEACCES
		return true
	}
	return strings.Contains(err.Error(), "Access is denied") || strings.Contains(err.Error(), "AccessDenied")
}
