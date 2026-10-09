//go:build windows

package process

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// TerminateChecked validates creation times on handles before ending anything.
// Keeping handles open prevents PID reuse from redirecting the operation.
func (c *windowsCollector) TerminateChecked(target Info, tree bool) error {
	if target.PID <= 4 || target.PID == uint32(os.Getpid()) || target.Started.IsZero() {
		return errors.New("cannot end this process safely")
	}
	rows, err := c.Snapshot()
	if err != nil {
		return err
	}
	byID := map[uint32]Info{}
	children := map[uint32][]uint32{}
	for _, r := range rows {
		byID[r.PID] = r
	}
	root, ok := byID[target.PID]
	if !ok || !root.Started.Equal(target.Started) {
		return errors.New("the selected process exited or its PID was reused")
	}
	for _, r := range rows {
		p, ok := byID[r.PPID]
		if ok && !r.Started.IsZero() && !p.Started.IsZero() && !r.Started.Before(p.Started) {
			children[r.PPID] = append(children[r.PPID], r.PID)
		}
	}
	ids := []uint32{target.PID}
	if tree {
		ids = append(ids, descendants(target.PID, children)...)
	}
	type owned struct {
		pid    uint32
		handle uintptr
	}
	handles := make([]owned, 0, len(ids))
	defer func() {
		for _, h := range handles {
			procClose.Call(h.handle)
		}
	}()
	for _, id := range ids {
		if id <= 4 || id == uint32(os.Getpid()) {
			return errors.New("process tree contains a protected target")
		}
		h, _, e := procOpen.Call(processTerminate|processQueryLimitedInformation, 0, uintptr(id))
		if h == 0 {
			return fmt.Errorf("PID %d: %w", id, e)
		}
		handles = append(handles, owned{id, h})
		var created, exited, kernel, user fileTime
		ok, _, e := procTimes.Call(h, uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
		if ok == 0 {
			return fmt.Errorf("PID %d identity: %w", id, e)
		}
		stamp := time.Unix(0, int64(fileTimeValue(created)-116444736000000000)*100)
		if !stamp.Equal(byID[id].Started) {
			return fmt.Errorf("PID %d was reused", id)
		}
	}
	var failures []error
	for i := len(handles) - 1; i >= 0; i-- {
		h := handles[i]
		r, _, e := procTerminate.Call(h.handle, 1)
		if r == 0 && e != syscall.Errno(0) {
			failures = append(failures, fmt.Errorf("PID %d: %w", h.pid, e))
		}
	}
	return errors.Join(failures...)
}
