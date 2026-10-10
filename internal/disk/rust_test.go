package disk

import "testing"

// rustSnapshot builds a tiny index: one Cargo project with debug+release
// profiles, plus a decoy "target" dir with no Cargo.toml next to it.
func rustSnapshot() *Snapshot {
	return &Snapshot{Root: 0, Nodes: []Node{
		{ID: 0, Parent: -1, Dir: true, Name: `C:\ws`, Children: []int{1, 8}},
		{ID: 1, Parent: 0, Dir: true, Name: "proj", Children: []int{2, 3}},
		{ID: 2, Parent: 1, Name: "Cargo.toml"},
		{ID: 3, Parent: 1, Dir: true, Name: "target", Allocated: 100, Children: []int{4, 5}},
		{ID: 4, Parent: 3, Dir: true, Name: "debug", Allocated: 70, Children: []int{6, 7}},
		{ID: 5, Parent: 3, Dir: true, Name: "release", Allocated: 30},
		{ID: 6, Parent: 4, Dir: true, Name: "incremental", Allocated: 10},
		{ID: 7, Parent: 4, Name: "app.exe", Allocated: 60},
		{ID: 8, Parent: 0, Dir: true, Name: "notrust", Children: []int{9}},
		{ID: 9, Parent: 8, Dir: true, Name: "target", Allocated: 999},
	}}
}

func TestFindRustTargetsTiers(t *testing.T) {
	rep := FindRustTargets(rustSnapshot(), nil)
	if rep.Projects != 1 {
		t.Fatalf("projects=%d, want 1 (decoy target must be skipped)", rep.Projects)
	}
	if got := rep.Tiers[0].Bytes; got != 10 {
		t.Fatalf("incremental=%d, want 10", got)
	}
	if got := rep.Tiers[1].Bytes; got != 70 {
		t.Fatalf("debug=%d, want 70", got)
	}
	if got := rep.Tiers[2].Bytes; got != 100 {
		t.Fatalf("target=%d, want 100", got)
	}
	if len(rep.Tiers[0].Dirs) != 1 || rep.Tiers[0].Dirs[0].Path != `C:\ws\proj\target\debug\incremental` {
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
	// Deleting target/debug also takes its incremental dir with it.
	rep.ApplyDeleted(map[string]uint64{`C:\ws\proj\target\debug`: 70})
	if rep.Projects != 1 {
		t.Fatalf("projects=%d, want 1", rep.Projects)
	}
	it := rep.Items[0]
	if it.Incr != 0 || it.Debug != 0 || it.Total != 30 {
		t.Fatalf("item after debug delete: %+v", it)
	}
	if rep.Tiers[0].Bytes != 0 || rep.Tiers[1].Bytes != 0 || rep.Tiers[2].Bytes != 100 {
		t.Fatalf("tiers after debug delete: %+v", rep.Tiers)
	}
	// Deleting the whole target removes the project row entirely.
	rep.ApplyDeleted(map[string]uint64{`C:\ws\proj\target`: 30})
	if rep.Projects != 0 || len(rep.Items) != 0 || rep.Tiers[2].Bytes != 0 {
		t.Fatalf("target delete left residue: %+v", rep)
	}
}
