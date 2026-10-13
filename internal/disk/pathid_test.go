package disk

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathIDBindVerify(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := BindPath(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if !id.Verify() {
		t.Fatal("freshly bound path must verify")
	}

	// Rebuilding the leaf under the same name is legitimate — a cache
	// dir recreated between scan and clean stays deletable.
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if !id.Verify() {
		t.Fatal("a rebuilt leaf is still the same cleanup target")
	}
}

func TestPathIDAncestorSwap(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("reparse fixture is Windows-only")
	}
	root := t.TempDir()
	mid := filepath.Join(root, "mid")
	target := filepath.Join(mid, "leaf")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := BindPath(target, false)
	if err != nil {
		t.Fatal(err)
	}
	// Swap an ancestor for a link to another tree — the deletion must
	// refuse, not follow the redirect.
	if err := os.RemoveAll(mid); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, mid); err != nil {
		t.Skipf("cannot create reparse point: %v", err)
	}
	if id.Verify() {
		t.Fatal("ancestor swapped for a link must fail verification")
	}
}

func TestPathIDLeafBecameLink(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("reparse fixture is Windows-only")
	}
	root := t.TempDir()
	target := filepath.Join(root, "leaf")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := BindPath(target, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("cannot create reparse point: %v", err)
	}
	if id.Verify() {
		t.Fatal("a leaf turned into a link must fail verification")
	}
}
