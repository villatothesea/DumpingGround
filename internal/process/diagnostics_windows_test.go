//go:build windows

package process

import (
	"os"
	"testing"
)

func TestHandleSearchCurrentProcess(t *testing.T) {
	svc := NewService()
	hi, ok := svc.Handles()
	if !ok {
		t.Fatal("handle inspector unavailable")
	}
	rows, err := svc.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(os.Getpid())
	found := false
	for _, r := range rows {
		if r.PID == pid {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("current process missing")
	}
	handles, err := hi.SearchHandles(pid, "", 256)
	if err != nil && len(handles) == 0 {
		t.Fatalf("handle search failed: %v", err)
	}
	if len(handles) == 0 {
		t.Fatal("no current-process handles")
	}
	t.Logf("handles=%d first=%+v", len(handles), handles[0])
}
func TestStackCurrentProcess(t *testing.T) {
	svc := NewService()
	si, ok := svc.Stacks()
	if !ok {
		t.Fatal("stack inspector unavailable")
	}
	pid := uint32(os.Getpid())
	// Toolhelp thread enumeration is accessed through the regular inspector.
	inspector, ok := svc.Inspect()
	if !ok {
		t.Fatal("inspector unavailable")
	}
	threads, err := inspector.Threads(pid)
	if err != nil || len(threads) == 0 {
		t.Fatalf("threads=%d err=%v", len(threads), err)
	}
	tid := threads[0].ID
	frames, err := si.Stack(pid, tid, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 1 {
		t.Fatal("empty stack")
	}
	t.Logf("thread=%d frames=%d first=%+v", tid, len(frames), frames[0])
}
