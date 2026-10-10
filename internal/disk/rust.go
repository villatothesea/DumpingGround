package disk

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RustTier is one cleanup level: a name, the directories to delete and the
// space deleting them frees. Tiers are cumulative by risk — each level
// deletes a strict superset of the previous one's purpose.
type RustTier struct {
	Name  string
	Note  string
	Dirs  []int
	Bytes uint64
}

// RustItem is one project's target/ dir with its per-tier breakdown.
type RustItem struct {
	Target      int // node id of the target/ dir
	Incr, Debug uint64
	Total       uint64
}

// RustReport summarizes Rust build artifacts inside a snapshot: target/
// directories that sit next to a Cargo.toml.
type RustReport struct {
	Projects int // target/ dirs found
	Tiers    [3]RustTier
	Items    []RustItem // per project, sorted by Total descending
}

// FindRustTargets walks the snapshot for directories named "target" whose
// parent holds a Cargo.toml, then buckets them into three cleanup tiers:
// incremental caches, whole debug builds, and whole target dirs. gone, when
// non-nil, reports paths already deleted so a re-analysis skips them.
func FindRustTargets(s *Snapshot, gone func(string) bool) *RustReport {
	rep := &RustReport{}
	rep.Tiers[0].Name, rep.Tiers[0].Note = "Incremental caches", "Compiler fingerprints and incremental state — rebuild costs almost nothing"
	rep.Tiers[1].Name, rep.Tiers[1].Note = "Debug builds", "Every target/debug dir — rebuilds on the next cargo build"
	rep.Tiers[2].Name, rep.Tiers[2].Note = "Everything", "Entire target/ dirs including release builds — slowest to rebuild"
	dead := func(id int) bool {
		return gone != nil && gone(s.FullPath(id))
	}
	for i := range s.Nodes {
		n := &s.Nodes[i]
		if !n.Dir || !strings.EqualFold(n.Name, "target") {
			continue
		}
		if !hasCargoToml(s, n.Parent) || dead(i) {
			continue
		}
		rep.Projects++
		rep.Tiers[2].Dirs = append(rep.Tiers[2].Dirs, i)
		rep.Tiers[2].Bytes += n.Allocated
		item := RustItem{Target: i, Total: n.Allocated}
		for _, prof := range n.Children {
			p := &s.Nodes[prof]
			if !p.Dir {
				continue
			}
			if strings.EqualFold(p.Name, "debug") && !dead(prof) {
				rep.Tiers[1].Dirs = append(rep.Tiers[1].Dirs, prof)
				rep.Tiers[1].Bytes += p.Allocated
				item.Debug += p.Allocated
			}
			for _, sub := range p.Children {
				if d := &s.Nodes[sub]; d.Dir && strings.EqualFold(d.Name, "incremental") && !dead(sub) {
					rep.Tiers[0].Dirs = append(rep.Tiers[0].Dirs, sub)
					rep.Tiers[0].Bytes += d.Allocated
					item.Incr += d.Allocated
				}
			}
		}
		rep.Items = append(rep.Items, item)
	}
	sort.Slice(rep.Items, func(a, b int) bool { return rep.Items[a].Total > rep.Items[b].Total })
	return rep
}

// hasCargoToml reports whether the dir node has a Cargo.toml child — the
// marker that a "target" sibling is a Rust build directory.
func hasCargoToml(s *Snapshot, dir int) bool {
	if dir < 0 || dir >= len(s.Nodes) {
		return false
	}
	for _, id := range s.Nodes[dir].Children {
		k := &s.Nodes[id]
		if !k.Dir && strings.EqualFold(k.Name, "cargo.toml") {
			return true
		}
	}
	return false
}

// SafeRustDelete checks that p still points at a directory with the expected
// base name before RemoveAll touches it — the index is stale by seconds, so
// this guards against a path that was renamed or replaced in between.
func SafeRustDelete(p string) error {
	base := strings.ToLower(filepath.Base(p))
	if base != "target" && base != "debug" && base != "incremental" {
		return errors.New("path is not a Rust artifact directory")
	}
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("path is not a directory")
	}
	return nil
}
