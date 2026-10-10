package disk

import (
	"path/filepath"
	"slices"
	"sort"
)

// USN reason bits (subset of USN_REASON_*).
const (
	usnDataMask      = 0x000000ff
	usnFileCreate    = 0x00000100
	usnFileDelete    = 0x00000200
	usnRenameOldName = 0x00001000
	usnRenameNewName = 0x00002000
	usnBasicInfo     = 0x00008000
	usnHardLink      = 0x00010000
	usnReparse       = 0x00100000
)

const attrDirectory = 0x10

// filteredAttrs mirrors the attribute mask skipEntry drops during a walk:
// reparse points, offline and recall placeholders.
const filteredAttrs = 0x400 | 0x1000 | 0x40000 | 0x400000

// USNEvent is one change-journal record reduced to what the snapshot needs.
// Reason may combine several bits for changes accumulated before close.
type USNEvent struct {
	FRN, ParentFRN uint64
	Name           string
	Attrs, Reason  uint32
}

// FileStatFunc measures a live filesystem entry. ok is false when the path
// can no longer be measured (typically: it was deleted again).
type FileStatFunc func(path string) (logical, allocated uint64, ok bool)

// ApplyUSN folds a batch of change-journal events into the snapshot in
// journal order and reports whether anything changed. stat may be nil for
// the platform default. Not safe for concurrent use; call it from the same
// goroutine that reads the snapshot.
func (s *Snapshot) ApplyUSN(events []USNEvent, stat FileStatFunc) bool {
	changed, _ := s.ApplyUSNDirs(events, stat)
	return changed
}

// ApplyUSNDirs applies a batch like ApplyUSN and also reports which
// directories gained or lost members, so a caller drawing only part of
// the tree can tell whether its visible slice actually changed.
func (s *Snapshot) ApplyUSNDirs(events []USNEvent, stat FileStatFunc) (bool, map[int]bool) {
	if stat == nil {
		stat = defaultUSNStat
	}
	changed := false
	touched := map[int]bool{}
	renames := map[uint64]USNEvent{}
	apply := func(e USNEvent) {
		switch {
		case e.Reason&usnFileCreate != 0 || e.Reason&usnHardLink != 0:
			if e.Attrs&filteredAttrs != 0 || s.usnFind(e.FRN, e.ParentFRN, e.Name) >= 0 {
				return
			}
			if id := s.usnInsert(e, stat); id >= 0 {
				touched[s.Nodes[id].Parent] = true
				changed = true
			}
		case e.Reason&usnRenameOldName != 0:
			renames[e.FRN] = e
		case e.Reason&usnRenameNewName != 0:
			old, ok := renames[e.FRN]
			delete(renames, e.FRN)
			if s.usnRename(e, old, ok, touched) {
				changed = true
			}
		case e.Reason&usnFileDelete != 0:
			id := s.usnFind(e.FRN, e.ParentFRN, e.Name)
			if id < 0 {
				return
			}
			touched[s.Nodes[id].Parent] = true
			s.usnRemove(id)
			changed = true
		case e.Reason&usnBasicInfo != 0 || e.Reason&usnReparse != 0:
			if e.Attrs&filteredAttrs != 0 {
				if id := s.usnFind(e.FRN, e.ParentFRN, e.Name); id >= 0 {
					touched[s.Nodes[id].Parent] = true
					s.usnRemove(id)
					changed = true
				}
			}
		case e.Reason&usnDataMask != 0:
			if s.usnRestat(e, stat) {
				changed = true
			}
		}
	}
	for _, e := range events {
		apply(e)
	}
	for p := range touched {
		kids := s.Nodes[p].Children
		sort.Slice(kids, func(a, b int) bool {
			x, y := s.Nodes[kids[a]], s.Nodes[kids[b]]
			if x.Allocated == y.Allocated {
				return x.Name < y.Name
			}
			return x.Allocated > y.Allocated
		})
	}
	return changed, touched
}

// frnInit maps file reference numbers to node IDs lazily; one FRN can own
// several nodes when a record carries multiple names (hard links).
func (s *Snapshot) frnInit() {
	if s.frns != nil {
		return
	}
	s.frns = make(map[uint64]int, len(s.Nodes))
	for i := range s.Nodes {
		if s.Nodes[i].FRN != 0 && (s.Nodes[i].Parent >= 0 || i == s.Root) {
			s.frnAdd(s.Nodes[i].FRN, i)
		}
	}
}

// frnEach visits every node a file reference number owns until f returns
// false — the single link first, then the extra hard links.
func (s *Snapshot) frnEach(frn uint64, f func(id int) bool) {
	s.frnInit()
	first, ok := s.frns[frn]
	if !ok || !f(first) {
		return
	}
	for _, id := range s.frnMore[frn] {
		if !f(id) {
			return
		}
	}
}

func (s *Snapshot) frnAdd(frn uint64, id int) {
	s.frnInit()
	if _, ok := s.frns[frn]; ok {
		if s.frnMore == nil {
			s.frnMore = make(map[uint64][]int)
		}
		s.frnMore[frn] = append(s.frnMore[frn], id)
		return
	}
	s.frns[frn] = id
}

func (s *Snapshot) frnDrop(frn uint64, id int) {
	s.frnInit()
	if first, ok := s.frns[frn]; ok && first == id {
		if more := s.frnMore[frn]; len(more) > 0 {
			s.frns[frn] = more[0]
			if s.frnMore[frn] = more[1:]; len(s.frnMore[frn]) == 0 {
				delete(s.frnMore, frn)
			}
		} else {
			delete(s.frns, frn)
		}
		return
	}
	if more := s.frnMore[frn]; len(more) > 0 {
		if i := slices.Index(more, id); i >= 0 {
			if s.frnMore[frn] = slices.Delete(more, i, i+1); len(s.frnMore[frn]) == 0 {
				delete(s.frnMore, frn)
			}
		}
	}
}

// usnDir resolves the directory node for a file reference number. The root
// node qualifies despite having no parent; tombstoned nodes do not.
func (s *Snapshot) usnDir(frn uint64) int {
	out := -1
	s.frnEach(frn, func(id int) bool {
		if n := &s.Nodes[id]; n.Dir && (n.Parent >= 0 || id == s.Root) {
			out = id
			return false
		}
		return true
	})
	return out
}

// usnFind locates the node representing one name (link) of a record.
func (s *Snapshot) usnFind(frn, parentFRN uint64, name string) int {
	out := -1
	s.frnEach(frn, func(id int) bool {
		n := &s.Nodes[id]
		if n.Parent < 0 || n.Name != name {
			return true
		}
		if n.Parent == s.Root || s.Nodes[n.Parent].FRN == parentFRN {
			out = id
			return false
		}
		return true
	})
	return out
}

// usnDelta shifts aggregate sizes on every ancestor of id. The walk is
// bounded: a stale journal record must never leave a parent cycle here,
// where it would loop forever on the UI goroutine.
func (s *Snapshot) usnDelta(id int, dBytes, dAlloc int64) {
	for p, steps := s.Nodes[id].Parent, 0; p >= 0 && steps < len(s.Nodes); steps++ {
		n := &s.Nodes[p]
		n.Bytes = uint64(int64(n.Bytes) + dBytes)
		n.Allocated = uint64(int64(n.Allocated) + dAlloc)
		p = n.Parent
	}
}

// usnHasAncestor reports whether id is among p's ancestors: the check that
// keeps a directory from being relinked beneath its own subtree.
func (s *Snapshot) usnHasAncestor(p, id int) bool {
	for a, steps := s.Nodes[p].Parent, 0; a >= 0 && steps < len(s.Nodes); steps++ {
		if a == id {
			return true
		}
		a = s.Nodes[a].Parent
	}
	return false
}

// usnInsert adds a node under the directory identified by e.ParentFRN.
func (s *Snapshot) usnInsert(e USNEvent, stat FileStatFunc) int {
	parent := s.usnDir(e.ParentFRN)
	if parent < 0 {
		return -1
	}
	id := len(s.Nodes)
	dir := e.Attrs&attrDirectory != 0
	s.frnInit()
	_, shared := s.frns[e.FRN]
	n := Node{ID: id, Parent: parent, Name: e.Name, Dir: dir, FRN: e.FRN}
	if !dir {
		logical, allocated, ok := stat(filepath.Join(s.FullPath(parent), e.Name))
		if ok {
			n.Bytes = logical
			if shared {
				n.Shared = true
			} else {
				n.Allocated = allocated
			}
		}
		s.Files++
		s.Bytes += n.Bytes
		s.Allocated += n.Allocated
	} else {
		s.Dirs++
	}
	s.Nodes = append(s.Nodes, n)
	s.Nodes[parent].Children = append(s.Nodes[parent].Children, id)
	s.frnAdd(e.FRN, id)
	s.usnDelta(id, int64(n.Bytes), int64(n.Allocated))
	return id
}

// usnRemove detaches a node and tombstones its whole subtree. When the
// counted link of a hard-linked file is removed, its allocation moves to a
// surviving link so volume totals stay consistent.
func (s *Snapshot) usnRemove(id int) {
	n := &s.Nodes[id]
	if n.Parent < 0 || n.Parent >= len(s.Nodes) {
		return
	}
	if !n.Dir && n.Allocated > 0 {
		sib := -1
		s.frnEach(n.FRN, func(x int) bool {
			if x != id && s.Nodes[x].Parent >= 0 {
				sib = x
				return false
			}
			return true
		})
		if sib >= 0 {
			s.Nodes[sib].Allocated = n.Allocated
			s.Nodes[sib].Shared = false
			s.usnDelta(sib, 0, int64(n.Allocated))
			n.Allocated = 0
		}
	}
	s.usnDelta(id, -int64(n.Bytes), -int64(n.Allocated))
	s.Bytes -= n.Bytes
	s.Allocated -= n.Allocated
	parent := &s.Nodes[n.Parent]
	if i := slices.Index(parent.Children, id); i >= 0 {
		parent.Children = slices.Delete(parent.Children, i, i+1)
	}
	stack := []int{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		c := &s.Nodes[cur]
		if c.Dir {
			s.Dirs--
		} else {
			s.Files--
		}
		if c.FRN != 0 {
			s.frnDrop(c.FRN, cur)
		}
		stack = append(stack, c.Children...)
		c.Name, c.FRN, c.Parent, c.Children = "", 0, -1, nil
		c.Bytes, c.Allocated = 0, 0
	}
}

// usnRename applies a new-name record; ok/old carry the paired old-name
// record when one preceded it in the batch.
func (s *Snapshot) usnRename(e, old USNEvent, ok bool, touched map[int]bool) bool {
	id := -1
	s.frnEach(e.FRN, func(cand int) bool {
		n := &s.Nodes[cand]
		if n.Parent < 0 {
			return true
		}
		pfrn := s.Nodes[n.Parent].FRN
		if ok {
			if n.Name == old.Name && (n.Parent == s.Root || pfrn == old.ParentFRN) {
				id = cand
				return false
			}
		} else if n.Name != e.Name || pfrn != e.ParentFRN {
			id = cand
			return false
		}
		return true
	})
	if id < 0 {
		return false
	}
	n := &s.Nodes[id]
	parent := s.usnDir(e.ParentFRN)
	if parent < 0 {
		// The target directory is not part of the index; drop the link.
		s.usnRemove(id)
		return true
	}
	if parent != n.Parent {
		if n.Dir && s.usnHasAncestor(parent, id) {
			// A move beneath the record's own subtree cannot be real; the
			// journal's parent FRN resolved to a stale descendant. Keep the
			// link where it is and apply only the rename, or the parent
			// chain becomes a cycle every ancestor walk loops inside.
			n.Name = e.Name
			touched[n.Parent] = true
			return true
		}
		oldParent := n.Parent
		s.usnDelta(id, -int64(n.Bytes), -int64(n.Allocated))
		p := &s.Nodes[oldParent]
		if i := slices.Index(p.Children, id); i >= 0 {
			p.Children = slices.Delete(p.Children, i, i+1)
		}
		n.Parent = parent
		s.Nodes[parent].Children = append(s.Nodes[parent].Children, id)
		s.usnDelta(id, int64(n.Bytes), int64(n.Allocated))
		touched[oldParent] = true
	}
	n.Name = e.Name
	touched[n.Parent] = true
	return true
}

// usnRestat re-measures a file after data changes and shifts aggregates.
func (s *Snapshot) usnRestat(e USNEvent, stat FileStatFunc) bool {
	id := s.usnFind(e.FRN, e.ParentFRN, e.Name)
	if id < 0 {
		return false
	}
	n := &s.Nodes[id]
	if n.Dir {
		return false
	}
	logical, allocated, ok := stat(filepath.Join(s.FullPath(n.Parent), n.Name))
	if !ok {
		return false
	}
	dBytes := int64(logical) - int64(n.Bytes)
	dAlloc := int64(0)
	if !n.Shared {
		dAlloc = int64(allocated) - int64(n.Allocated)
		n.Allocated = allocated
	}
	if dBytes == 0 && dAlloc == 0 {
		return false
	}
	n.Bytes = logical
	s.Bytes = uint64(int64(s.Bytes) + dBytes)
	s.Allocated = uint64(int64(s.Allocated) + dAlloc)
	s.usnDelta(id, dBytes, dAlloc)
	return true
}
