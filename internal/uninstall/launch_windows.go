//go:build windows

package uninstall

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

var procShellExecEx = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo mirrors SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}

const (
	seeMaskNoCloseProcess = 0x40
	errElevationRequired  = 740 // ERROR_ELEVATION_REQUIRED
	errOperationCancelled = 1223
)

// Process is a launched uninstaller the caller can wait on. Windows
// cannot hand back an exec.Cmd — the process came from ShellExecute —
// so the wait is a plain handle wait.
type Process struct{ handle uintptr }

// Wait blocks until the uninstaller process exits, then releases the
// handle. The exit code is not meaningful — Inno-style parents exit 0
// while a child window still runs, which is why the caller watches
// the registry entry instead.
func (p *Process) Wait() error {
	h := syscall.Handle(p.handle)
	syscall.WaitForSingleObject(h, syscall.INFINITE)
	syscall.CloseHandle(h)
	return nil
}

// hideWindow keeps console-subsystem uninstall commands (rare, but
// batch-driven uninstallers exist) from flashing a black cmd window.
// Used for processes this package creates directly (regedit).
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

// launch starts the entry's uninstall command through the shell —
// the same launch Apps Settings performs. Three wins over
// CreateProcess: the argument tail reaches the uninstaller verbatim
// (no split-and-requote mangling), an app whose manifest demands
// elevation gets the UAC prompt it needs instead of error 740, and
// MSI/composite commands resolve the way the shell runs them.
func launch(e *Entry) (*Process, error) {
	exe, tail := splitExeTail(e.Uninstall)
	if exe == "" {
		return nil, fmt.Errorf("no uninstall command")
	}
	p, err := shellLaunch(exe, tail, "open")
	if errno, ok := err.(syscall.Errno); ok && errno == errElevationRequired {
		// The uninstaller demands admin — ask through the shell's
		// own runas verb rather than failing the launch.
		p, err = shellLaunch(exe, tail, "runas")
	}
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == errOperationCancelled {
			return nil, fmt.Errorf("cancelled at the UAC prompt")
		}
		return nil, err
	}
	return &Process{handle: p}, nil
}

// shellLaunch is one ShellExecuteEx call; the returned process handle
// stays open for the caller to wait on.
func shellLaunch(exe, params, verb string) (uintptr, error) {
	exeP, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	var parP *uint16
	if params != "" {
		if parP, err = syscall.UTF16PtrFromString(params); err != nil {
			return 0, err
		}
	}
	verbP, err := syscall.UTF16PtrFromString(verb)
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verbP,
		lpFile:       exeP,
		lpParameters: parP,
		nShow:        1, // SW_SHOWNORMAL
	}
	r, _, e := procShellExecEx.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, e
	}
	return info.hProcess, nil
}

// splitExeTail separates the command's executable from its argument
// tail — the tail stays raw so quoted arguments reach the process
// exactly as the registry stored them.
func splitExeTail(s string) (exe, tail string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if s[0] == '"' {
		if end := strings.Index(s[1:], `"`); end >= 0 {
			return s[1 : 1+end], strings.TrimSpace(s[1+end+1:])
		}
		return strings.Trim(s, `"`), ""
	}
	if i := strings.Index(strings.ToLower(s), ".exe"); i >= 0 {
		return s[:i+4], strings.TrimSpace(s[i+4:])
	}
	f := strings.Fields(s)
	return f[0], strings.TrimSpace(s[len(f[0]):])
}
