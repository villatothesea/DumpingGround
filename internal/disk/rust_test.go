package disk

import (
	"os"
	"path/filepath"
	"testing"
)

// rustSnapshot builds a tiny index: one Cargo project with debug+release
// profiles, a doc dir, loose files and a nested check62/debug layout like
// a second CARGO_TARGET_DIR — plus two decoy "target" dirs: one with no
// Cargo.toml or cargo traces, and one orphan that has Cargo's marker
// file but no project left.
func rustSnapshot() *Snapshot {
	return &Snapshot{Root: 0, Nodes: []Node{
		{ID: 0, Parent: -1, Dir: true, Name: `C:\ws`, Children: []int{1, 8, 14}},
		{ID: 1, Parent: 0, Dir: true, Name: "proj", Children: []int{2, 3}},
		{ID: 2, Parent: 1, Name: "Cargo.toml"},
		{ID: 3, Parent: 1, Dir: true, Name: "target", Allocated: 216, Children: []int{4, 5, 10, 15, 16, 17, 20}},
		{ID: 4, Parent: 3, Dir: true, Name: "debug", Allocated: 70, Children: []int{6, 7}},
		{ID: 5, Parent: 3, Dir: true, Name: "release", Allocated: 30, Children: []int{11}},
		{ID: 6, Parent: 4, Dir: true, Name: "incremental", Allocated: 10},
		{ID: 7, Parent: 4, Name: "app.exe", Allocated: 60},
		{ID: 8, Parent: 0, Dir: true, Name: "notrust", Children: []int{9}},
		{ID: 9, Parent: 8, Dir: true, Name: "target", Allocated: 999},
		{ID: 10, Parent: 3, Dir: true, Name: "check62", Allocated: 100, Children: []int{12}},
		{ID: 11, Parent: 5, Dir: true, Name: "incremental", Allocated: 5},
		{ID: 12, Parent: 10, Dir: true, Name: "debug", Allocated: 80, Children: []int{13}},
		{ID: 13, Parent: 12, Dir: true, Name: "incremental", Allocated: 40},
		{ID: 14, Parent: 0, Dir: true, Name: "gone-proj", Children: []int{18}},
		{ID: 15, Parent: 3, Dir: true, Name: "doc", Allocated: 12},
		{ID: 16, Parent: 3, Name: "debug.log", Allocated: 4},
		{ID: 17, Parent: 3, Name: "CACHEDIR.TAG", Allocated: 0},
		{ID: 18, Parent: 14, Dir: true, Name: "target", Allocated: 77, Children: []int{19}},
		{ID: 19, Parent: 18, Name: "CACHEDIR.TAG"},
		{ID: 20, Parent: 3, Name: ".future-incompat-report.json", Allocated: 0},
	}}
}

// isolateCaches points the shared-cache measurement at empty temp dirs so
// tests do not walk the host's real ~/.cargo and ~/.rustup.
func isolateCaches(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("RUSTUP_HOME", t.TempDir())
}

func TestFindRustTargetsCats(t *testing.T) {
	isolateCaches(t)
	rep := FindRustTargets(rustSnapshot(), nil, nil)
	if rep.Projects != 1 {
		t.Fatalf("projects=%d, want 1 (decoy target must be skipped)", rep.Projects)
	}
	if got := rep.Cats[RustCatIncr].Bytes; got != 55 {
		t.Fatalf("incremental=%d, want 55 (10 debug + 5 release + 40 nested)", got)
	}
	if got := rep.Cats[RustCatDebug].Bytes; got != 150 {
		t.Fatalf("debug=%d, want 150 (70 top-level + 80 nested check62)", got)
	}
	if got := rep.Cats[RustCatRelease].Bytes; got != 30 {
		t.Fatalf("release=%d, want 30", got)
	}
	if got := rep.Cats[RustCatDoc].Bytes; got != 12 {
		t.Fatalf("doc=%d, want 12", got)
	}
	// One stray file; CACHEDIR.TAG is Cargo's own and must not count.
	if got := rep.Cats[RustCatStray].Bytes; got != 4 {
		t.Fatalf("stray=%d, want 4", got)
	}
	stray := rep.Cats[RustCatStray].Dirs[0]
	if !stray.File || stray.Base != "debug.log" {
		t.Fatalf("stray entry %+v must be flagged file named debug.log", stray)
	}
	if len(rep.Cats[RustCatIncr].Dirs) != 3 {
		t.Fatalf("incremental dirs=%v", rep.Cats[RustCatIncr].Dirs)
	}
	// The orphan: CACHEDIR.TAG inside but no Cargo.toml beside it.
	if len(rep.Cats[RustCatOrphans].Dirs) != 1 || rep.Cats[RustCatOrphans].Dirs[0].Path != `C:\ws\gone-proj\target` {
		t.Fatalf("orphans=%v, want the gone-proj target", rep.Cats[RustCatOrphans].Dirs)
	}
	it := rep.Items[0]
	if it.Incr != 55 || it.Debug != 150 || it.Rel != 30 || it.Doc != 12 || it.Stray != 4 || it.Total != 216 {
		t.Fatalf("item fields: %+v", it)
	}
}

func TestFindRustTargetsSkipsDeleted(t *testing.T) {
	isolateCaches(t)
	rep := FindRustTargets(rustSnapshot(), func(p string) bool {
		return p == `C:\ws\proj\target` || p == `C:\ws\gone-proj\target`
	}, nil)
	if rep.Projects != 0 || len(rep.Items) != 0 {
		t.Fatalf("deleted target still reported: %+v", rep)
	}
	if len(rep.Cats[RustCatOrphans].Dirs) != 0 {
		t.Fatalf("deleted orphan still reported")
	}
}

func TestApplyDeletedCascades(t *testing.T) {
	isolateCaches(t)
	rep := FindRustTargets(rustSnapshot(), nil, nil)
	// Deleting target/debug also takes its incremental dir with it; the
	// nested check62/debug stays.
	rep.ApplyDeleted(map[string]uint64{`C:\ws\proj\target\debug`: 70})
	if rep.Projects != 1 {
		t.Fatalf("projects=%d, want 1", rep.Projects)
	}
	it := rep.Items[0]
	if it.Incr != 45 || it.Debug != 80 || it.Total != 146 {
		t.Fatalf("item after debug delete: %+v", it)
	}
	if rep.Cats[RustCatIncr].Bytes != 45 || rep.Cats[RustCatDebug].Bytes != 80 {
		t.Fatalf("cats after debug delete: %+v", rep.Cats)
	}
	// Deleting the whole target removes the project row entirely.
	rep.ApplyDeleted(map[string]uint64{`C:\ws\proj\target`: 146})
	if rep.Projects != 0 || len(rep.Items) != 0 {
		t.Fatalf("target delete left residue: %+v", rep)
	}
	for c := RustCatIncr; c <= RustCatStray; c++ {
		if rep.Cats[c].Bytes != 0 || len(rep.Cats[c].Dirs) != 0 {
			t.Fatalf("cat %d kept residue after target delete", c)
		}
	}
}

func TestSharedCachesAndToolchains(t *testing.T) {
	// A fake user profile: cargo caches plus two toolchains, one the
	// default — only the non-default one may be offered.
	cargoHome := t.TempDir()
	rustupHome := t.TempDir()
	for _, dir := range []string{`registry`, `git`} {
		p := filepath.Join(cargoHome, dir)
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, "blob"), make([]byte, 10), 0o644)
	}
	for _, tc := range []string{`stable-x86_64-pc-windows-msvc`, `1.98.0-x86_64-pc-windows-msvc`} {
		p := filepath.Join(rustupHome, "toolchains", tc)
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, "rustc.exe"), make([]byte, 20), 0o644)
	}
	os.WriteFile(filepath.Join(rustupHome, "settings.toml"),
		[]byte("default_toolchain = \"stable-x86_64-pc-windows-msvc\"\n"), 0o644)
	t.Setenv("CARGO_HOME", cargoHome)
	t.Setenv("RUSTUP_HOME", rustupHome)
	rep := FindRustTargets(rustSnapshot(), nil, nil)
	if rep.Cats[RustCatRegistry].Bytes != 10 || rep.Cats[RustCatGit].Bytes != 10 {
		t.Fatalf("shared caches: %+v %+v", rep.Cats[RustCatRegistry], rep.Cats[RustCatGit])
	}
	tcs := rep.Cats[RustCatToolchains]
	if len(tcs.Dirs) != 1 || tcs.Dirs[0].Base != "1.98.0-x86_64-pc-windows-msvc" || tcs.Bytes != 20 {
		t.Fatalf("toolchains %v — want only the non-default one", tcs.Dirs)
	}
}

func TestToolchainsHiddenWithoutDefault(t *testing.T) {
	// No readable default_toolchain: offering any toolchain would risk
	// the active one, so the category must stay empty.
	rustupHome := t.TempDir()
	p := filepath.Join(rustupHome, "toolchains", "stable-x86_64-pc-windows-msvc")
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "rustc.exe"), make([]byte, 20), 0o644)
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("RUSTUP_HOME", rustupHome)
	rep := FindRustTargets(rustSnapshot(), nil, nil)
	if len(rep.Cats[RustCatToolchains].Dirs) != 0 {
		t.Fatalf("toolchains offered without a known default: %v", rep.Cats[RustCatToolchains].Dirs)
	}
}

func TestDedupRustDirs(t *testing.T) {
	dirs := DedupRustDirs([]RustDir{
		{Path: `C:\ws\proj\target\debug\incremental`, Bytes: 10},
		{Path: `C:\ws\proj\target\debug`, Bytes: 70},
		{Path: `C:\ws\proj\target`, Bytes: 216},
		{Path: `C:\ws\other\target\debug`, Bytes: 5},
	})
	if len(dirs) != 2 {
		t.Fatalf("dedup kept %v, want target + other/debug", dirs)
	}
	if dirs[0].Path != `C:\ws\other\target\debug` || dirs[1].Path != `C:\ws\proj\target` {
		t.Fatalf("dedup order: %v", dirs)
	}
}

func TestSafeRustEntry(t *testing.T) {
	dir := t.TempDir()
	if err := SafeRustEntry(dir, "", false); err != nil {
		t.Fatalf("plain dir rejected: %v", err)
	}
	if err := SafeRustEntry(dir, "nope", false); err == nil {
		t.Fatalf("mismatched base accepted")
	}
	if err := SafeRustEntry(dir, "", true); err == nil {
		t.Fatalf("dir accepted as file")
	}
	if err := SafeRustEntry(dir+`-missing`, "", false); err == nil {
		t.Fatalf("missing path accepted")
	}
}
