//go:build windows

package process

import "github.com/dumpingground/dumpingground/internal/elevate"

// openProc tries OpenProcess directly first; on access denial the
// elevated worker opens it — possibly after enabling SeDebug — and
// donates the handle. Same contract as procOpen: 0 + error on failure.
func openProc(pid uint32, mask uintptr) (uintptr, error) {
	h, _, e := procOpen.Call(mask, 0, uintptr(pid))
	if h != 0 {
		return h, nil
	}
	if dh, err := elevate.OpenProcessHandle(pid, uint32(mask)); err == nil {
		return dh, nil
	}
	return 0, e
}
