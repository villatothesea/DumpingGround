package disk

import "testing"

func usnFRN(record uint64) uint64 { return record | 1<<48 }

// usnFixture builds root/dir/a.txt with correct aggregates.
func usnFixture() *Snapshot {
	s := &Snapshot{
		Root:  0,
		Files: 1, Dirs: 2, Bytes: 100, Allocated: 128,
		Nodes: []Node{
			{ID: 0, Parent: -1, Name: `D:\`, Path: `D:\`, Dir: true, FRN: usnFRN(5), Bytes: 100, Allocated: 128, Children: []int{1}},
			{ID: 1, Parent: 0, Name: "dir", Dir: true, FRN: usnFRN(6), Bytes: 100, Allocated: 128, Children: []int{2}},
			{ID: 2, Parent: 1, Name: "a.txt", Bytes: 100, Allocated: 128, FRN: usnFRN(7)},
		},
	}
	return s
}

func usnFakeStat(sizes map[string][2]uint64) FileStatFunc {
	return func(p string) (uint64, uint64, bool) {
		v, ok := sizes[p]
		return v[0], v[1], ok
	}
}

func TestApplyUSNCreateAndDelete(t *testing.T) {
	s := usnFixture()
	dir := usnFRN(6)
	// A create directly under the volume root must resolve the root node
	// as its parent even though the root has no parent of its own.
	stat := usnFakeStat(map[string][2]uint64{`D:\rootfile`: {8, 8}})
	if !s.ApplyUSN([]USNEvent{{FRN: usnFRN(9), ParentFRN: usnFRN(5), Name: "rootfile", Attrs: 0x20, Reason: usnFileCreate}}, stat) {
		t.Fatal("create under root was not applied")
	}
	if s.Nodes[0].Children[len(s.Nodes[0].Children)-1] != len(s.Nodes)-1 || s.Files != 2 {
		t.Fatalf("root create not linked: %+v", s.Nodes[0])
	}
	s = usnFixture()
	stat = usnFakeStat(map[string][2]uint64{`D:\dir\b.txt`: {200, 256}})
	if !s.ApplyUSN([]USNEvent{{FRN: usnFRN(8), ParentFRN: dir, Name: "b.txt", Attrs: 0x20, Reason: usnFileCreate}}, stat) {
		t.Fatal("create was not applied")
	}
	if s.Files != 2 || s.Bytes != 300 || s.Allocated != 384 {
		t.Fatalf("after create: files=%d bytes=%d allocated=%d", s.Files, s.Bytes, s.Allocated)
	}
	if s.Nodes[1].Bytes != 300 || len(s.Nodes[1].Children) != 2 {
		t.Fatalf("dir not updated: %+v", s.Nodes[1])
	}
	if !s.ApplyUSN([]USNEvent{{FRN: usnFRN(8), ParentFRN: dir, Name: "b.txt", Reason: usnFileDelete}}, stat) {
		t.Fatal("delete was not applied")
	}
	if s.Files != 1 || s.Bytes != 100 || s.Allocated != 128 || len(s.Nodes[1].Children) != 1 {
		t.Fatalf("after delete: files=%d bytes=%d allocated=%d kids=%v", s.Files, s.Bytes, s.Allocated, s.Nodes[1].Children)
	}
}

func TestApplyUSNDataAndRename(t *testing.T) {
	s := usnFixture()
	dir := usnFRN(6)
	stat := usnFakeStat(map[string][2]uint64{`D:\dir\a.txt`: {150, 160}})
	events := []USNEvent{
		{FRN: usnFRN(7), ParentFRN: dir, Name: "a.txt", Reason: 0x2 /* USN_REASON_DATA_EXTEND */},
		{FRN: usnFRN(7), ParentFRN: dir, Name: "a.txt", Reason: usnRenameOldName},
		{FRN: usnFRN(7), ParentFRN: dir, Name: "c.txt", Reason: usnRenameNewName},
	}
	if !s.ApplyUSN(events, stat) {
		t.Fatal("events were not applied")
	}
	n := s.Nodes[2]
	if n.Name != "c.txt" || n.Bytes != 150 || n.Allocated != 160 {
		t.Fatalf("node after rename+resize: %+v", n)
	}
	if s.Bytes != 150 || s.Allocated != 160 || s.Nodes[1].Bytes != 150 {
		t.Fatalf("aggregates: bytes=%d allocated=%d dir=%d", s.Bytes, s.Allocated, s.Nodes[1].Bytes)
	}
}

func TestApplyUSNDirDeleteDropsSubtree(t *testing.T) {
	s := usnFixture()
	if !s.ApplyUSN([]USNEvent{{FRN: usnFRN(6), ParentFRN: usnFRN(5), Name: "dir", Reason: usnFileDelete}}, nil) {
		t.Fatal("dir delete was not applied")
	}
	if s.Files != 0 || s.Dirs != 1 || s.Bytes != 0 || s.Allocated != 0 || len(s.Nodes[0].Children) != 0 {
		t.Fatalf("after dir delete: %+v root=%+v", s, s.Nodes[0])
	}
}

func TestApplyUSNHardLinkReattribution(t *testing.T) {
	s := usnFixture()
	// Two links to the same record: id 2 counted, id 3 shared elsewhere.
	s.Nodes = append(s.Nodes,
		Node{ID: 3, Parent: 0, Name: "other", Dir: true, FRN: usnFRN(9), Bytes: 0, Allocated: 0, Children: []int{4}},
		Node{ID: 4, Parent: 3, Name: "link2", Bytes: 100, Allocated: 0, Shared: true, FRN: usnFRN(7)})
	s.Nodes[0].Children = append(s.Nodes[0].Children, 3)
	s.Nodes[2].Parent = 1
	s.Files = 2
	s.Nodes[0].Bytes += 0
	s.Nodes[3].Bytes = 0
	dir := usnFRN(6)
	// Deleting the counted link must move allocation to link2.
	if !s.ApplyUSN([]USNEvent{{FRN: usnFRN(7), ParentFRN: dir, Name: "a.txt", Reason: usnFileDelete}}, nil) {
		t.Fatal("counted-link delete was not applied")
	}
	sib := s.Nodes[4]
	if !sib.Shared && sib.Allocated == 128 {
		return
	}
	t.Fatalf("allocation not reattributed: %+v", sib)
}

func TestApplyUSNFilteredCreateSkipped(t *testing.T) {
	s := usnFixture()
	ev := USNEvent{FRN: usnFRN(8), ParentFRN: usnFRN(6), Name: "ph", Attrs: 0x1000 | 0x10, Reason: usnFileCreate}
	if s.ApplyUSN([]USNEvent{ev}, nil) || s.Files != 1 {
		t.Fatal("placeholder create must be ignored")
	}
	// An existing entry that turns into a placeholder is removed.
	ev = USNEvent{FRN: usnFRN(7), ParentFRN: usnFRN(6), Name: "a.txt", Attrs: 0x1000 | 0x20, Reason: usnBasicInfo}
	if !s.ApplyUSN([]USNEvent{ev}, nil) || s.Files != 0 {
		t.Fatal("placeholder transition must drop the node")
	}
}
