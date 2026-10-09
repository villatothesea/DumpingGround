//go:build windows

package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAllocatedSizeAndHardLinks(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "original.bin")
	data := make([]byte, 8193)
	if err := os.WriteFile(first, data, 0644); err != nil {
		t.Fatal(err)
	}
	used, known := allocation(first, int64(len(data)))
	if !known || used < uint64(len(data)) {
		t.Fatalf("allocation=%d known=%v", used, known)
	}
	if err := os.Link(first, filepath.Join(root, "second-link.bin")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	got, err := (&Scanner{}).Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 2*uint64(len(data)) || got.Allocated != used {
		t.Fatalf("logical=%d allocated=%d want=%d", got.Bytes, got.Allocated, used)
	}
	shared := 0
	for _, n := range got.Nodes {
		if n.Shared {
			shared++
		}
	}
	if shared != 1 {
		t.Fatalf("shared nodes=%d", shared)
	}
}
func TestAllocationMissingFileIsMarkedEstimate(t *testing.T) {
	n, known := allocation(filepath.Join(t.TempDir(), "missing"), 123)
	if known || n != 123 {
		t.Fatalf("got %d %v", n, known)
	}
}
