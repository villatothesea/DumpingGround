//go:build windows

package process

import (
	"os"
	"testing"
)

func TestSnapshotIncludesCurrentProcess(t *testing.T) {
	rows, err := NewService().Snapshot()
	if err != nil { t.Fatal(err) }
	pid := uint32(os.Getpid())
	for _, row := range rows {
		if row.PID == pid {
			if row.Name == "" { t.Fatal("current process has no name") }
			return
		}
	}
	t.Fatalf("snapshot did not include current process %d", pid)
}
