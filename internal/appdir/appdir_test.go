package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortableDirWins(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "dg", "DumpingGround.exe")
	legacy := t.TempDir()
	got := resolve(exe, legacy)
	want := filepath.Join(filepath.Dir(exe), ".data")
	if got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}

func TestLegacyWhenExeUnknown(t *testing.T) {
	legacy := t.TempDir()
	if got := resolve("", legacy); got != legacy {
		t.Fatalf("resolve = %q, want %q", got, legacy)
	}
}

// When the portable folder cannot exist — the exe's "directory" is a
// file here — resolution falls back to the per-user folder. (Windows
// ignores the read-only bit on directories, so that is not a way to
// make this case.)
func TestFallsBackWhenNotWritable(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := t.TempDir()
	if got := resolve(filepath.Join(notADir, "dg.exe"), legacy); got != legacy {
		t.Fatalf("resolve = %q, want %q", got, legacy)
	}
}

// A freshly created .data folder adopts the old per-user state —
// logs, undo records and history all move with the exe.
func TestMigrateOnFirstPortableUse(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "dg", "DumpingGround.exe")
	legacy := t.TempDir()
	for p, body := range map[string]string{
		"startup-undo.json":      "{}",
		"uninstalled.json":       "[]",
		"pending-sweep.json":     "[]",
		"logs/dumpingground.log": "line\n",
	} {
		full := filepath.Join(legacy, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := resolve(exe, legacy)
	for p := range map[string]bool{
		"startup-undo.json":      true,
		"uninstalled.json":       true,
		"pending-sweep.json":     true,
		"logs/dumpingground.log": true,
	} {
		if _, err := os.Stat(filepath.Join(got, p)); err != nil {
			t.Errorf("migrated %q missing: %v", p, err)
		}
	}
}

// A .data folder that already has files keeps them — migration only
// happens when it is first created.
func TestNoMigrateIntoExistingData(t *testing.T) {
	exeDir := t.TempDir()
	portable := filepath.Join(exeDir, ".data")
	if err := os.MkdirAll(portable, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(portable, "startup-undo.json"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacy, "uninstalled.json"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := resolve(filepath.Join(exeDir, "dg.exe"), legacy)
	if b, _ := os.ReadFile(filepath.Join(got, "startup-undo.json")); string(b) != "new" {
		t.Error("existing portable file was overwritten")
	}
	if _, err := os.Stat(filepath.Join(got, "uninstalled.json")); err == nil {
		t.Error("legacy file migrated into an existing .data folder")
	}
}

func TestSetDirPinsAndRestores(t *testing.T) {
	pinned := t.TempDir()
	SetDir(pinned)
	if got := Dir(); got != pinned {
		t.Fatalf("Dir = %q, want %q", got, pinned)
	}
	SetDir("")
	if got := Dir(); got == "" {
		t.Fatal("Dir did not resolve after restore")
	}
}
