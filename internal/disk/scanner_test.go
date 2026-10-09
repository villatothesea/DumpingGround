package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanner(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "folder", "b.txt"), []byte("world!"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Progress, 32)
	s := &Scanner{}
	got, err := s.Scan(context.Background(), root, ch)
	if err != nil {
		t.Fatal(err)
	}
	progress := len(ch)
	if got.Files != 2 || got.Bytes != 11 || progress == 0 {
		t.Fatalf("snapshot = %+v, progress = %d", got, progress)
	}
}

func TestScannerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&Scanner{}).Scan(ctx, t.TempDir(), nil)
	if err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
}
