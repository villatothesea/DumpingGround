//go:build windows

package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUSNJournalLive exercises the real change journal end to end: scan a
// volume, start the watcher, touch the filesystem, and confirm events apply.
// Requires administrator rights; skipped unless DG_USN_LIVE is set.
func TestUSNJournalLive(t *testing.T) {
	root := os.Getenv("DG_USN_LIVE")
	if root == "" {
		t.Skip("set DG_USN_LIVE=D:\\ to run against a real volume")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	snap, err := ProbeMFT(ctx, root)
	if err != nil {
		t.Fatalf("MFT probe: %v", err)
	}
	t.Logf("snapshot: %d files, journal=%x usn=%d", snap.Files, snap.JournalID, snap.JournalUSN)
	w, err := snap.WatchJournal()
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer w.Close()

	dir := filepath.Join(root, "dg-usn-live")
	file := filepath.Join(dir, "probe.bin")
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)
	os.WriteFile(file, []byte("hello journal"), 0644)

	// Unrelated volume activity applies changes constantly; keep draining
	// until cond holds or the deadline passes.
	wait := func(what string, cond func() bool) {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && !cond() {
			ev, err := w.Next(ctx)
			if err != nil {
				t.Fatalf("watcher: %v", err)
			}
			snap.ApplyUSN(ev, nil)
		}
		if !cond() {
			t.Fatalf("%s: still false after 10s", what)
		}
	}
	got := -1
	wait("probe.bin create", func() bool {
		got = -1
		for i := range snap.Nodes {
			if snap.Nodes[i].Name == "probe.bin" && snap.Nodes[i].Bytes == 13 {
				got = i
			}
		}
		return got >= 0
	})
	os.Remove(file)
	wait("probe.bin delete", func() bool { return snap.Nodes[got].Parent < 0 })
}
