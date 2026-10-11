package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogAndPrune(t *testing.T) {
	SetDir(t.TempDir())
	defer SetDir("")

	Section("Test op")
	Logf("removed %s", "x")
	out := Read()
	if !strings.Contains(out, "== Test op ==") || !strings.Contains(out, "removed x") {
		t.Fatalf("log missing lines:\n%s", out)
	}

	// An old log past the retention window is pruned.
	old := filepath.Join(Dir(), "dumpingground-2000-01-01.log")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().AddDate(0, 0, -40)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	Prune()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old log should be pruned")
	}
	if _, err := os.Stat(path()); err != nil {
		t.Fatal("today's log must survive pruning")
	}
}
