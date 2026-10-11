package disk

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// junkFixture builds a fake %TEMP%-style root: one stale file, one
// fresh file, a Low dir and a nested empty dir.
func junkFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	stale := filepath.Join(root, "stale.tmp")
	fresh := filepath.Join(root, "fresh.tmp")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Low", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func junkKidsDir(t *testing.T, rep *JunkReport, cat int) JunkDir {
	t.Helper()
	dirs := rep.Cats[cat].Dirs
	if len(dirs) != 1 {
		t.Fatalf("expected one row, got %d", len(dirs))
	}
	return dirs[0]
}

func TestJunkKidsAgeGate(t *testing.T) {
	root := junkFixture(t)
	rep := &JunkReport{}
	rep.junkKids(JunkCatUserTemp, root, junkTempAge)
	d := junkKidsDir(t, rep, JunkCatUserTemp)
	if d.Files != 1 || d.Bytes != 3 {
		t.Fatalf("only the stale file should count, got %d files %d bytes", d.Files, d.Bytes)
	}
	if d.Mode != JunkKids || d.Base != filepath.Base(root) {
		t.Fatalf("row should be kids-mode on the root, got mode %d base %q", d.Mode, d.Base)
	}
}

func TestJunkCleanKidsKeepsFreshAndRoot(t *testing.T) {
	root := junkFixture(t)
	rep := &JunkReport{}
	rep.junkKids(JunkCatUserTemp, root, junkTempAge)
	d := junkKidsDir(t, rep, JunkCatUserTemp)
	freed, removed, skipped, err := JunkClean(d)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 || skipped != 0 || freed != 3 {
		t.Fatalf("removed %d skipped %d freed %d, want 1/0/3", removed, skipped, freed)
	}
	for _, keep := range []string{root, filepath.Join(root, "fresh.tmp"), filepath.Join(root, "Low")} {
		if _, err := os.Lstat(keep); err != nil {
			t.Fatalf("%s must survive kids-mode cleaning", keep)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "stale.tmp")); err == nil {
		t.Fatal("stale file should be gone")
	}
	// The emptied plain dir is swept; the whitelisted Low tree is not.
	if _, err := os.Lstat(filepath.Join(root, "emptydir")); err == nil {
		t.Fatal("empty dir should have been swept")
	}
}

func TestJunkCleanKidsSkipsReparse(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction fixture is Windows-only")
	}
	root := t.TempDir()
	outside := t.TempDir()
	marker := filepath.Join(outside, "keep-me.txt")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	junc := filepath.Join(root, "link")
	if err := os.Symlink(outside, junc); err != nil {
		t.Skipf("cannot create reparse point: %v", err)
	}
	d := JunkDir{Path: root, Base: filepath.Base(root), Mode: JunkKids}
	if _, _, _, err := JunkClean(d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(marker); err != nil {
		t.Fatal("deletion followed the junction into the outside dir")
	}
	if _, err := os.Lstat(junc); err != nil {
		t.Fatal("the junction entry itself must not be removed")
	}
}

func TestJunkCleanKidsRejectsReplacedPath(t *testing.T) {
	root := junkFixture(t)
	rep := &JunkReport{}
	rep.junkKids(JunkCatUserTemp, root, junkTempAge)
	d := junkKidsDir(t, rep, JunkCatUserTemp)
	// Remove the root and drop a decoy with the same name in place.
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := JunkClean(d); err != nil {
		t.Fatal(err) // same name + same kind — a legit re-clean
	}
	// A file where the dir was expected is refused.
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("decoy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := JunkClean(d); err == nil {
		t.Fatal("a file replacing the dir must fail the entry check")
	}
}

func TestSafeJunkEntryBasename(t *testing.T) {
	root := t.TempDir()
	good := JunkDir{Path: root, Base: filepath.Base(root)}
	if err := SafeJunkEntry(good); err != nil {
		t.Fatalf("a matching dir should pass: %v", err)
	}
	bad := JunkDir{Path: root, Base: "not-the-name"}
	if err := SafeJunkEntry(bad); err == nil {
		t.Fatal("a renamed root must be refused")
	}
	missing := JunkDir{Path: filepath.Join(root, "gone"), Base: "gone"}
	if err := SafeJunkEntry(missing); err == nil {
		t.Fatal("a vanished path must be refused")
	}
}

func TestJunkCleanWholeRemovesDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Windows.old")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), []byte("zz"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := JunkDir{Path: dir, Bytes: 2, Base: "Windows.old", Mode: JunkWhole}
	freed, removed, _, err := JunkClean(d)
	if err != nil {
		t.Fatal(err)
	}
	if freed != 2 || removed != 1 {
		t.Fatalf("freed %d removed %d, want 2/1", freed, removed)
	}
	if _, err := os.Lstat(dir); err == nil {
		t.Fatal("the dir should be gone")
	}
}

func TestJunkCleanShellGuardsSkip(t *testing.T) {
	// A row marked skipped — e.g. its browser is running — must not be
	// touched even when asked directly.
	d := JunkDir{Path: "all drives", Base: "", Mode: JunkShell, Skip: "Chrome is running"}
	_, _, sk, err := JunkClean(d)
	if err != nil || sk != 1 {
		t.Fatalf("skipped row: err %v skipped %d", err, sk)
	}
}

func TestJunkBrowserGuard(t *testing.T) {
	local := t.TempDir()
	roaming := t.TempDir()
	cache := filepath.Join(local, `Google\Chrome\User Data`, "Default", "Cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "c.bin"), []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := &JunkReport{}
	rep.junkBrowsers(local, roaming, func(name string) bool { return name == "chrome.exe" })
	dirs := rep.Cats[JunkCatBrowser].Dirs
	if len(dirs) != 1 {
		t.Fatalf("expected one cache row, got %d", len(dirs))
	}
	if dirs[0].Skip == "" {
		t.Fatal("a running browser must mark its rows skipped")
	}
	rep = &JunkReport{}
	rep.junkBrowsers(local, roaming, func(string) bool { return false })
	dirs = rep.Cats[JunkCatBrowser].Dirs
	if len(dirs) != 1 || dirs[0].Skip != "" {
		t.Fatalf("idle browser row should be cleanable, got %+v", dirs)
	}
}

func TestJunkDumpWarn(t *testing.T) {
	if w := junkDumpWarn("memory.dmp"); !strings.Contains(w, "kernel") {
		t.Fatalf("memory.dmp should mention kernel debugging, got %q", w)
	}
	w := junkDumpWarn("OwlCommander.exe.277692.dmp")
	if !strings.Contains(w, "OwlCommander") {
		t.Fatalf("app dump should name the crashed app, got %q", w)
	}
	if w := junkDumpWarn("WATCHDOG-20261006-2302.dmp"); !strings.Contains(w, "Kernel") {
		t.Fatalf("kernel dump should say kernel evidence, got %q", w)
	}
}

func TestJunkKidsEmptyRoot(t *testing.T) {
	root := t.TempDir()
	rep := &JunkReport{}
	rep.junkKids(JunkCatUserTemp, root, 0)
	if len(rep.Cats[JunkCatUserTemp].Dirs) != 0 {
		t.Fatal("an empty dir produces no row")
	}
	rep.junkKids(JunkCatUserTemp, filepath.Join(root, "missing"), 0)
	if len(rep.Cats[JunkCatUserTemp].Dirs) != 0 {
		t.Fatal("a missing dir produces no row")
	}
}
