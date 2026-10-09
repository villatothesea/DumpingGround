//go:build windows

package disk

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Opt-in because this exercises the metadata of a real system volume.
func TestLiveVolumeScanAndFallback(t *testing.T) {
	root := os.Getenv("DG_TEST_VOLUME")
	if root == "" {
		t.Skip("set DG_TEST_VOLUME to a drive root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := (&Scanner{}).Scan(ctx, root, nil)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatalf("no partial or complete result: %v", err)
	}
	if out.Method != "Directory walk" && out.Method != "NTFS MFT" {
		t.Fatalf("unknown method %q", out.Method)
	}
	if out.Method == "Directory walk" && out.Fallback == "" {
		t.Fatal("fallback reason missing")
	}
	t.Logf("method=%s files=%d elapsed=%s fallback=%s", out.Method, out.Files, out.Duration, out.Fallback)
}
