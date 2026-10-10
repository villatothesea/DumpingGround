//go:build windows

package disk

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	shFileOpDLL   = syscall.NewLazyDLL("shell32.dll")
	procSHFileOp  = shFileOpDLL.NewProc("SHFileOperationW")
	coInitDLL     = syscall.NewLazyDLL("ole32.dll")
	procCoInitEx  = coInitDLL.NewProc("CoInitializeEx")
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
// the shell. It returns an error when the shell refuses — the caller
// decides whether to fall back to a plain RemoveAll — so the recycle
// path never silently becomes a permanent delete.
func RecycleFileOrDir(path string) error {
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
