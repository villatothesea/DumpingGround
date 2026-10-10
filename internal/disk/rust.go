package disk

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RustDir is one directory a cleanup tier deletes, with the bytes the
// index measured for it.
type RustDir struct {
	Path  string
	Bytes uint64
}

// RustTier is one cleanup level: a name, the directories to delete and the
// space deleting them frees. Tiers are cumulative by risk — each level
// deletes a strict superset of the previous one's purpose.
type RustTier struct {
	Name  string
	Note  string
	Dirs  []RustDir
	Bytes uint64
}

// RustItem is one project's target/ dir with its per-tier breakdown. Path
// is the target/ dir itself; the project lives in its parent.
type RustItem struct {
	Path        string
	Incr, Debug uint64
	Total       uint64
}

// RustReport summarizes Rust build artifacts inside a snapshot: target/
// directories that sit next to a Cargo.toml. It is self-contained — paths
// and sizes only, no node ids — so it stays browsable and cleanable after
// the index that produced it is released.
type RustReport struct {
	Projects int    // target/ dirs found
	Scanned  uint64 // the bytes the % column compares each item to
	Tiers    [3]RustTier
	Items    []RustItem // per project, sorted by Total descending
}

// ApplyDeleted drops everything a finished deletion removed and recomputes
// the rest, so the report stays accurate without an index. deleted maps
// each RemoveAll'd dir to the bytes it held; dirs inside a deleted dir go
// with it.
func (rep *RustReport) ApplyDeleted(deleted map[string]uint64) {
	if len(deleted) == 0 {
		return
	}
	sep := string(filepath.Separator)
	under := func(p string) bool {
		for d := range deleted {
			if p == d || strings.HasPrefix(p, d+sep) {
				return true
			}
		}
		return false
	}
	for t := range rep.Tiers {
		tier := &rep.Tiers[t]
		keep, bytes := tier.Dirs[:0], uint64(0)
		for _, d := range tier.Dirs {
			if !under(d.Path) {
				keep = append(keep, d)
				bytes += d.Bytes
			}
		}
		tier.Dirs, tier.Bytes = keep, bytes
	}
	items := rep.Items[:0]
	for _, it := range rep.Items {
		if under(it.Path) {
			continue
		}
		for d, b := range deleted {
			if strings.HasPrefix(d, it.Path+sep) {
				it.Total -= min(it.Total, b)
			}
		}
		// Incr and Debug recompute from the surviving tier dirs, so an
		// incremental dir that went down with its debug dir leaves both.
		it.Incr, it.Debug = 0, 0
		for _, d := range rep.Tiers[0].Dirs {
			if strings.HasPrefix(d.Path, it.Path+sep) {
				it.Incr += d.Bytes
			}
		}
		for _, d := range rep.Tiers[1].Dirs {
			if strings.HasPrefix(d.Path, it.Path+sep) {
				it.Debug += d.Bytes
			}
		}
		items = append(items, it)
	}
	rep.Items, rep.Projects = items, len(items)
}

// FindRustTargets walks the snapshot for directories named "target" whose
// parent holds a Cargo.toml, then buckets them into three cleanup tiers:
// incremental caches, whole debug builds, and whole target dirs. gone, when
// non-nil, reports paths already deleted so a re-analysis skips them.
func FindRustTargets(s *Snapshot, gone func(string) bool) *RustReport {
	rep := &RustReport{Scanned: s.Allocated}
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
		rep.Tiers[2].Dirs = append(rep.Tiers[2].Dirs, RustDir{s.FullPath(i), n.Allocated})
		rep.Tiers[2].Bytes += n.Allocated
		item := RustItem{Path: s.FullPath(i), Total: n.Allocated}
		for _, prof := range n.Children {
			p := &s.Nodes[prof]
			if !p.Dir {
				continue
			}
			if strings.EqualFold(p.Name, "debug") && !dead(prof) {
				rep.Tiers[1].Dirs = append(rep.Tiers[1].Dirs, RustDir{s.FullPath(prof), p.Allocated})
				rep.Tiers[1].Bytes += p.Allocated
				item.Debug += p.Allocated
			}
			for _, sub := range p.Children {
				if d := &s.Nodes[sub]; d.Dir && strings.EqualFold(d.Name, "incremental") && !dead(sub) {
					rep.Tiers[0].Dirs = append(rep.Tiers[0].Dirs, RustDir{s.FullPath(sub), d.Allocated})
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
