package disk

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Cleanup risk levels, safest first. Review items never appear in the
// preset plans — they need a human look before anything is deleted.
const (
	RustRiskSafe = iota
	RustRiskModerate
	RustRiskCareful
	RustRiskReview
)

// Cleanup category indices in RustReport.Cats, fixed order. The first
// five live inside project target dirs; the rest are shared locations
// that exist regardless of which drive was scanned.
const (
	RustCatIncr = iota
	RustCatDoc
	RustCatDebug
	RustCatRelease
	RustCatStray
	RustCatRegistry
	RustCatGit
	RustCatToolchains
	RustCatOrphans
	RustCatCount
)

// RustDir is one path a cleanup category removes, with the bytes measured
// for it. Base records the expected base name and File whether it is a
// loose file rather than a directory — both are revalidated before
// deletion so a path that changed underneath the stale report cannot be
// removed by mistake.
type RustDir struct {
	Path  string
	Bytes uint64
	Base  string
	File  bool
}

// RustCat is one cleanup category: a name, what deleting it costs, a
// risk level, and the paths it covers.
type RustCat struct {
	Name  string
	Note  string
	Risk  int
	Dirs  []RustDir
	Bytes uint64
}

// RustItem is one project's target/ dir with its per-category breakdown.
// Path is the target/ dir itself; the project lives in its parent. Idle
// is the days since the project was last touched — a source edit or a
// build — or -1 when nothing could be dated.
type RustItem struct {
	Path                         string
	Incr, Doc, Debug, Rel, Stray uint64
	Total                        uint64
	Idle                         int
}

// RustReport summarizes Rust build artifacts: per-category path lists
// plus one row per project target dir. It is self-contained — paths and
// sizes only, no node ids — so it stays browsable and cleanable after
// the index that produced it is released.
type RustReport struct {
	Projects int    // project target/ dirs found
	Scanned  uint64 // the bytes the % column compares each item to
	Cats     [RustCatCount]RustCat
	Items    []RustItem // per project, sorted by Total descending
}

// ApplyDeleted drops everything a finished deletion removed and recomputes
// the rest, so the report stays accurate without an index. deleted maps
// each removed path to the bytes it held; paths inside a deleted dir go
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
	for c := range rep.Cats {
		cat := &rep.Cats[c]
		keep, bytes := cat.Dirs[:0], uint64(0)
		for _, d := range cat.Dirs {
			if !under(d.Path) {
				keep = append(keep, d)
				bytes += d.Bytes
			}
		}
		cat.Dirs, cat.Bytes = keep, bytes
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
		// The per-category columns recompute from the surviving category
		// dirs, so an incremental dir that went down with its debug dir
		// leaves both counts.
		it.Incr, it.Doc, it.Debug, it.Rel, it.Stray = 0, 0, 0, 0, 0
		fields := []*uint64{&it.Incr, &it.Doc, &it.Debug, &it.Rel, &it.Stray}
		for c := RustCatIncr; c <= RustCatStray; c++ {
			for _, d := range rep.Cats[c].Dirs {
				if strings.HasPrefix(d.Path, it.Path+sep) {
					*fields[c] += d.Bytes
				}
			}
		}
		items = append(items, it)
	}
	rep.Items, rep.Projects = items, len(items)
}

// DedupRustDirs drops paths nested inside another selected path, so a
// batch delete never tries to remove a dir already gone with its parent —
// debug contains incremental, target contains everything.
func DedupRustDirs(dirs []RustDir) []RustDir {
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Path < dirs[j].Path })
	out, tops := dirs[:0], []string{}
	sep := string(filepath.Separator)
	for _, d := range dirs {
		nested := false
		for _, tp := range tops {
			if strings.EqualFold(d.Path, tp) || strings.HasPrefix(d.Path, tp+sep) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, d)
			tops = append(tops, d.Path)
		}
	}
	return out
}

// Rust analysis progress stages passed to FindRustTargets' progress
// callback: the index walk first, then the on-disk cache measurement.
const (
	RustStageIndex = iota
	RustStageCaches
)

// FindRustTargets walks the snapshot for directories named "target",
// sorts them into Cargo projects (a Cargo.toml sits next to them),
// orphans (cargo-built but the project marker is gone) and decoys, then
// buckets project contents into the in-target categories. Shared caches
// — ~/.cargo registry and git checkouts, ~/.rustup toolchains — are
// measured directly since they live under the user profile no matter
// which drive was scanned. gone, when non-nil, reports paths already
// deleted so a re-analysis skips them. progress, when non-nil, reports
// (stage, nodes visited, targets found) as the walk proceeds.
func FindRustTargets(s *Snapshot, gone func(string) bool, progress func(stage, visited, found int)) *RustReport {
	rep := &RustReport{Scanned: s.Allocated}
	cats := [RustCatCount]struct {
		name, note string
		risk       int
	}{
		{"Incremental caches", "Compiler fingerprints — rebuild costs almost nothing", RustRiskSafe},
		{"Doc builds", "cargo doc output — regenerates on demand", RustRiskSafe},
		{"Debug builds", "target/debug dirs — rebuild on the next cargo build", RustRiskModerate},
		{"Release builds", "Optimized binaries — slowest to rebuild", RustRiskCareful},
		{"Stray files", "Loose files inside target/ Cargo did not put there", RustRiskReview},
		{"Registry cache", "~/.cargo/registry — downloaded crate sources, re-fetched on demand", RustRiskSafe},
		{"Git checkouts", "~/.cargo/git — git dependencies, re-fetched on demand", RustRiskSafe},
		{"Old toolchains", "rustup toolchains other than the default — reinstall via rustup", RustRiskModerate},
		{"Orphan targets", "Cargo-built target dirs whose project marker is gone", RustRiskReview},
	}
	for i, n := range cats {
		rep.Cats[i].Name, rep.Cats[i].Note, rep.Cats[i].Risk = n.name, n.note, n.risk
	}
	add := func(cat int, path string, bytes uint64, base string, file bool) {
		rep.Cats[cat].Dirs = append(rep.Cats[cat].Dirs, RustDir{Path: path, Bytes: bytes, Base: base, File: file})
		rep.Cats[cat].Bytes += bytes
	}
	dead := func(id int) bool {
		return gone != nil && gone(s.FullPath(id))
	}
	claimed := map[int]bool{} // node ids of target dirs already handled
	inside := func(id int) bool {
		for p := s.Nodes[id].Parent; p >= 0; p = s.Nodes[p].Parent {
			if claimed[p] {
				return true
			}
		}
		return false
	}
	for i := range s.Nodes {
		if progress != nil && i&(1<<16-1) == 0 {
			progress(RustStageIndex, i, rep.Projects)
		}
		n := &s.Nodes[i]
		if !n.Dir || !strings.EqualFold(n.Name, "target") || dead(i) || inside(i) {
			continue
		}
		claimed[i] = true
		if !hasCargoToml(s, n.Parent) {
			// No Cargo.toml next to it: either a decoy that merely shares
			// the name, or a real Cargo target whose project is gone.
			// Cargo's own marker files tell the two apart.
			if cargoMarkers(s, i) {
				add(RustCatOrphans, s.FullPath(i), n.Allocated, "target", false)
			}
			continue
		}
		rep.Projects++
		path := s.FullPath(i)
		item := RustItem{Path: path, Total: n.Allocated, Idle: rustIdleDays(path)}
		// Cargo puts incremental/ under every profile dir — and profiles
		// themselves can nest under a target triple or a nested
		// CARGO_TARGET_DIR (check62/debug/incremental,
		// aarch64-linux-android/mobile/incremental), so the name search
		// covers the whole subtree. debug/release/doc stay at depth ≤2:
		// deeper dirs with those names are more likely a build script's
		// than a profile's.
		type frame struct {
			id, depth int
		}
		stack := []frame{{i, 0}}
		for len(stack) > 0 {
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, sub := range s.Nodes[f.id].Children {
				d := &s.Nodes[sub]
				if dead(sub) {
					continue
				}
				if !d.Dir {
					// Loose files at the target root are not Cargo's —
					// logs, scripts and dumps a developer dropped there.
					// Cargo keeps three root files of its own: the cache
					// marker, the rustc probe cache and the future-incompat
					// report it writes when deps warn.
					if f.id == i {
						name := strings.ToLower(d.Name)
						if name != "cachedir.tag" && name != ".rustc_info.json" && name != ".future-incompat-report.json" {
							add(RustCatStray, s.FullPath(sub), d.Allocated, d.Name, true)
							item.Stray += d.Allocated
						}
					}
					continue
				}
				switch strings.ToLower(d.Name) {
				case "incremental":
					add(RustCatIncr, s.FullPath(sub), d.Allocated, d.Name, false)
					item.Incr += d.Allocated
					continue // incremental dirs nest nothing worth listing
				case "debug", "release", "doc":
					if f.depth >= 2 {
						break
					}
					cat, field := RustCatDebug, &item.Debug
					switch d.Name[0] {
					case 'r', 'R':
						cat, field = RustCatRelease, &item.Rel
					case 'd', 'D':
						if strings.EqualFold(d.Name, "doc") {
							cat, field = RustCatDoc, &item.Doc
						}
					}
					add(cat, s.FullPath(sub), d.Allocated, d.Name, false)
					*field += d.Allocated
				}
				stack = append(stack, frame{sub, f.depth + 1})
			}
		}
		rep.Items = append(rep.Items, item)
		if progress != nil {
			progress(RustStageIndex, i+1, rep.Projects)
		}
	}
	sort.Slice(rep.Items, func(a, b int) bool { return rep.Items[a].Total > rep.Items[b].Total })
	if progress != nil {
		progress(RustStageIndex, len(s.Nodes), rep.Projects)
		progress(RustStageCaches, 0, rep.Projects)
	}
	rep.rustSharedCaches(gone)
	return rep
}

// rustSharedCaches measures the user-profile cargo and rustup dirs — the
// parts of a Rust install that no drive scan sees when the project tree
// lives elsewhere.
func (rep *RustReport) rustSharedCaches(gone func(string) bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cargoHome := os.Getenv("CARGO_HOME")
	if cargoHome == "" {
		cargoHome = filepath.Join(home, ".cargo")
	}
	for _, c := range []struct {
		cat int
		dir string
	}{
		{RustCatRegistry, filepath.Join(cargoHome, "registry")},
		{RustCatGit, filepath.Join(cargoHome, "git")},
	} {
		if gone != nil && gone(c.dir) {
			continue
		}
		if bytes, ok := duDir(c.dir); ok && bytes > 0 {
			rep.Cats[c.cat].Dirs = append(rep.Cats[c.cat].Dirs, RustDir{Path: c.dir, Bytes: bytes, Base: filepath.Base(c.dir)})
			rep.Cats[c.cat].Bytes += bytes
		}
	}
	// Toolchains: only the non-default ones are offered — deleting the
	// active toolchain would break every build until it is reinstalled.
	// If the default cannot be read, the category stays empty rather than
	// risk the wrong dir.
	rustupHome := os.Getenv("RUSTUP_HOME")
	if rustupHome == "" {
		rustupHome = filepath.Join(home, ".rustup")
	}
	protected := rustProtectedToolchains(rustupHome)
	if len(protected) == 0 {
		return
	}
	tcDir := filepath.Join(rustupHome, "toolchains")
	entries, err := os.ReadDir(tcDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || toolchainProtected(e.Name(), protected) {
			continue
		}
		p := filepath.Join(tcDir, e.Name())
		if gone != nil && gone(p) {
			continue
		}
		if bytes, ok := duDir(p); ok && bytes > 0 {
			rep.Cats[RustCatToolchains].Dirs = append(rep.Cats[RustCatToolchains].Dirs, RustDir{Path: p, Bytes: bytes, Base: e.Name()})
			rep.Cats[RustCatToolchains].Bytes += bytes
		}
	}
}

// rustProtectedToolchains collects every name a toolchain deletion
// must never touch: the settings.toml default, the RUSTUP_TOOLCHAIN
// environment override and every [overrides] mapping. Channel short
// names ("stable") also protect their installed long form
// ("stable-x86_64-pc-windows-msvc") — see toolchainProtected.
func rustProtectedToolchains(rustupHome string) map[string]bool {
	out := map[string]bool{}
	if v := os.Getenv("RUSTUP_TOOLCHAIN"); v != "" {
		out[strings.ToLower(v)] = true
	}
	data, err := os.ReadFile(filepath.Join(rustupHome, "settings.toml"))
	if err != nil {
		return out
	}
	overrides := false
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "[") {
			overrides = strings.HasPrefix(ln, "[overrides")
			continue
		}
		i := strings.Index(ln, "=")
		if i < 0 {
			continue
		}
		key := strings.TrimSpace(ln[:i])
		val := strings.Trim(strings.TrimSpace(ln[i+1:]), `"'`)
		if key == "default_toolchain" && val != "" {
			out[strings.ToLower(val)] = true
		} else if overrides && val != "" {
			out[strings.ToLower(val)] = true
		}
	}
	return out
}

// toolchainProtected reports whether an installed toolchain dir is in
// use. An exact match covers full names ("nightly-2024-01-01-...");
// a channel name covers its installed long form by prefix — "stable"
// protects "stable-x86_64-pc-windows-msvc".
func toolchainProtected(dir string, protected map[string]bool) bool {
	d := strings.ToLower(dir)
	if protected[d] {
		return true
	}
	for p := range protected {
		if strings.HasPrefix(d, p+"-") {
			return true
		}
	}
	return false
}

// duDir walks a directory tree and sums file sizes — logical bytes, close
// enough for cache dirs the index never saw.
func duDir(path string) (uint64, bool) {
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return 0, false
	}
	var total uint64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.Size() > 0 {
			total += uint64(fi.Size())
		}
		return nil
	})
	return total, true
}

// cargoMarkers reports whether a "target" dir shows Cargo's own traces —
// a .fingerprint dir or CACHEDIR.TAG — meaning it once held a real build
// even though no Cargo.toml sits next to it now.
func cargoMarkers(s *Snapshot, target int) bool {
	type frame struct {
		id, depth int
	}
	stack := []frame{{target, 0}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, sub := range s.Nodes[f.id].Children {
			d := &s.Nodes[sub]
			name := strings.ToLower(d.Name)
			if !d.Dir && (name == "cachedir.tag" || name == ".rustc_info.json") {
				return true
			}
			if d.Dir && name == ".fingerprint" {
				return true
			}
			if d.Dir && f.depth < 3 {
				stack = append(stack, frame{sub, f.depth + 1})
			}
		}
	}
	return false
}

// rustIdleDays estimates how long a project has sat untouched: the newest
// of the last source change — a shallow walk of the project minus heavy
// dirs — and the last build, dated by the fingerprint dirs cargo refreshes
// every compile. -1 means nothing on disk could be dated.
func rustIdleDays(target string) int {
	proj := filepath.Dir(target)
	newest := time.Time{}
	mark := func(p string) {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	// The last build: every profile's .fingerprint dir updates on compile.
	mark(target)
	for _, fp := range []string{`*\*.fingerprint`, `*\*\*.fingerprint`} {
		if matches, err := filepath.Glob(filepath.Join(target, fp)); err == nil {
			for _, m := range matches {
				mark(m)
			}
		}
	}
	// The last source edit, skipping dirs that dominate the walk without
	// saying anything about the project's own activity.
	skip := map[string]bool{"target": true, ".git": true, "node_modules": true, "vendor": true}
	steps := 0
	filepath.WalkDir(proj, func(p string, d os.DirEntry, err error) error {
		if err != nil || steps > 5000 {
			return filepath.SkipAll
		}
		if d.IsDir() && p != proj && skip[strings.ToLower(d.Name())] {
			return filepath.SkipDir
		}
		steps++
		if d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	if newest.IsZero() {
		return -1
	}
	return int(time.Since(newest).Hours() / 24)
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

// SafeRustEntry checks that path still exists and still matches what the
// report collected — expected base name, file vs directory — before
// deletion touches it. The report is stale by seconds, so this guards
// against a path that was renamed or replaced in between.
func SafeRustEntry(path, base string, file bool) error {
	if base != "" && !strings.EqualFold(filepath.Base(path), base) {
		return errors.New("path no longer looks like the expected artifact")
	}
	if junkPathReparse(path) {
		return errors.New("refusing a reparse point")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if file {
		if info.IsDir() {
			return errors.New("expected a file, found a directory")
		}
	} else if !info.IsDir() {
		return errors.New("path is not a directory")
	}
	return nil
}
