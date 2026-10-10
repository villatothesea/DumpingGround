package disk

import "testing"

// rustSnapshot builds a tiny index: one Cargo project with debug+release
// profiles plus a nested check62/debug layout like a second
// CARGO_TARGET_DIR, and a decoy "target" dir with no Cargo.toml next to
// it.
func rustSnapshot() *Snapshot {
	return &Snapshot{Root: 0, Nodes: []Node{
		{ID: 0, Parent: -1, Dir: true, Name: `C:\ws`, Children: []int{1, 8}},
		{ID: 1, Parent: 0, Dir: true, Name: "proj", Children: []int{2, 3}},
		{ID: 2, Parent: 1, Name: "Cargo.toml"},
		{ID: 3, Parent: 1, Dir: true, Name: "target", Allocated: 200, Children: []int{4, 5, 10}},
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
	}}
}

func TestFindRustTargetsTiers(t *testing.T) {
	rep := FindRustTargets(rustSnapshot(), nil)
	if rep.Projects != 1 {
		t.Fatalf("projects=%d, want 1 (decoy target must be skipped)", rep.Projects)
	}
	if got := rep.Tiers[0].Bytes; got != 55 {
		t.Fatalf("incremental=%d, want 55 (10 debug + 5 release + 40 nested)", got)
	}
	if got := rep.Tiers[1].Bytes; got != 150 {
		t.Fatalf("debug=%d, want 150 (70 top-level + 80 nested check62)", got)
	}
	if got := rep.Tiers[2].Bytes; got != 200 {
		t.Fatalf("target=%d, want 200", got)
	}
	if len(rep.Tiers[0].Dirs) != 3 {
		t.Fatalf("incremental dirs=%v", rep.Tiers[0].Dirs)
	}
}

func TestFindRustTargetsSkipsDeleted(t *testing.T) {
	rep := FindRustTargets(rustSnapshot(), func(p string) bool {
		return p == `C:\ws\proj\target`
	})
	if rep.Projects != 0 || rep.Tiers[2].Bytes != 0 {
		t.Fatalf("deleted target still reported: %+v", rep)
	}
}

func TestApplyDeletedCascades(t *testing.T) {
	rep := FindRustTargets(rustSnapshot(), nil)
	// Deleting target/debug also takes its incremental dir with it; the
	// nested check62/debug stays.
	rep.ApplyDeleted(map[string]uint64{`C:\ws\proj\target\debug`: 70})
	if rep.Projects != 1 {
		t.Fatalf("projects=%d, want 1", rep.Projects)
	}
	it := rep.Items[0]
	if it.Incr != 45 || it.Debug != 80 || it.Total != 130 {
		t.Fatalf("item after debug delete: %+v", it)
	}
	if rep.Tiers[0].Bytes != 45 || rep.Tiers[1].Bytes != 80 || rep.Tiers[2].Bytes != 200 {
		t.Fatalf("tiers after debug delete: %+v", rep.Tiers)
	}
	// Deleting the whole target removes the project row entirely.
	rep.ApplyDeleted(map[string]uint64{`C:\ws\proj\target`: 130})
	if rep.Projects != 0 || len(rep.Items) != 0 || rep.Tiers[2].Bytes != 0 {
		t.Fatalf("target delete left residue: %+v", rep)
	}
}
