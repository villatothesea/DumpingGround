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

func TestLogEscape(t *testing.T) {
	SetDir(t.TempDir())
	defer SetDir("")

	// A name carrying a newline must not forge a second log line.
	Logf("removed %s", "ok\n[00:00:00.000] forged evil")
	Logf("tab\tand ctrl%cbell", 0x07)
	out := Read()
	if strings.Contains(out, "\n[00:00:00.000] forged") {
		t.Fatalf("newline in a value forged a log line:\n%s", out)
	}
	if !strings.Contains(out, `ok\n[00:00:00.000] forged evil`) {
		t.Fatalf("newline should be escaped:\n%s", out)
	}
	if !strings.Contains(out, `tab\tand ctrl\x07bell`) {
		t.Fatalf("control bytes should be escaped:\n%s", out)
	}
	// Backslashes and ordinary text pass through untouched.
	Logf("path %s", `C:\Temp\x`)
	out = Read()
	if !strings.Contains(out, `C:\Temp\x`) {
		t.Fatalf("backslashes must stay literal:\n%s", out)
	}
}
