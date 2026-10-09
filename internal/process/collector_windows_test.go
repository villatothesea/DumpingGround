//go:build windows

package process

import (
	"os"
	"testing"
)

func TestInspectCurrentProcess(t *testing.T) {
	svc := NewService()
	inspector, ok := svc.Inspect()
	if !ok {
		t.Fatal("missing inspector")
	}
	pid := uint32(os.Getpid())
	modules, err := inspector.Modules(pid)
	if err != nil || len(modules) == 0 {
		t.Fatalf("modules=%d error=%v", len(modules), err)
	}
	threads, err := inspector.Threads(pid)
	if err != nil || len(threads) == 0 {
		t.Fatalf("threads=%d error=%v", len(threads), err)
	}
	props, err := inspector.(PropertyInspector).Properties(pid)
	if err != nil {
		t.Fatal(err)
	}
	if props.Path == "" || props.Command == "" || props.User == "" || props.Handles == 0 {
		t.Fatalf("incomplete properties: %+v", props)
	}
}

func TestSnapshotIncludesCurrentProcess(t *testing.T) {
	rows, err := NewService().Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(os.Getpid())
	for _, row := range rows {
		if row.PID == pid {
			if row.Name == "" {
				t.Fatal("current process has no name")
			}
			return
		}
	}
	t.Fatalf("snapshot did not include current process %d", pid)
}
