package uninstall

import (
	"os"
	"path/filepath"
	"strings"
)

// Confidence is how sure the scanner is that an item belongs to the
// app — the leftover review list pre-checks Confirmed and Likely and
// leaves Review items for the user to tick explicitly.
type Confidence uint8

const (
	// Confirmed items come straight from the registry record: the
	// install folder, the uninstall key itself, startup values that
	// point into the install folder.
	Confirmed Confidence = iota
	// Likely items match the app's (or publisher's) exact directory
	// name in a well-known user-data root, or a vendor registry tree.
	Likely
	// Review items are similar-but-not-exact matches — shown for
	// context, never preselected for removal.
	Review
)

// Kind distinguishes what a leftover removal means.
type Kind uint8

const (
	KindDir      Kind = iota // a filesystem directory (or file) — removed via the Recycle Bin
	KindRegKey               // a whole registry key tree — exported to a .reg backup first
	KindRegValue             // one value inside a key (Run/RunOnce entries)
)

// Item is one leftover candidate. Path is a filesystem path or, for
// registry items, the full hive path ("HKCU\Software\…").
type Item struct {
	Path       string
	Kind       Kind
	Confidence Confidence
	Why        string
	Bytes      uint64
	Reg32      bool   // registry items in the 32-bit (WOW6432) view
	RegValue   string // KindRegValue only: the value's name
}

// Registry reports whether removing this item edits the registry
// rather than the filesystem.
func (i Item) Registry() bool { return i.Kind != KindDir }

// Footprint lists an installed app's traces — its install folder,
// user-data directories, registry keys, startup entries and shortcuts.
// all is the full installed-app list, used to suppress anything that
// still belongs to another program. Read-only; nothing is removed.
func Footprint(e *Entry, all []Entry) []Item {
	items := fsFootprint(e, all, appRoots(), shortcutRoots())
	items = append(items, regFootprint(e)...)
	return items
}

// fsFootprint scans filesystem locations: the install folder,
// well-known app-data roots and shortcut folders.
func fsFootprint(e *Entry, all []Entry, roots, shortcuts []string) []Item {
	var out []Item
	nName := norm(e.Name)
	nPub := norm(e.Publisher)
	// The app's significant first word, for prefix matching dirs like
	// "Docker" that hold "Docker Desktop" data.
	first := ""
	if w := strings.Fields(e.Name); len(w) > 0 {
		first = norm(w[0])
	}

	// The install folder itself — the registry said so.
	if e.Location != "" && isDir(e.Location) {
		out = append(out, Item{
			Path: e.Location, Kind: KindDir, Confidence: Confirmed,
			Why: "InstallLocation recorded in the registry",
		})
	}

	for _, root := range roots {
		kids, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, kid := range kids {
			if !kid.IsDir() || isReparse(kid) {
				continue
			}
			p := filepath.Join(root, kid.Name())
			dn := norm(kid.Name())
			switch {
			case dn == nName:
				if claimed(p, e, all) {
					continue
				}
				out = append(out, Item{Path: p, Kind: KindDir, Confidence: Likely,
					Why: "user-data folder named after the app"})
			case dn == nPub && nPub != "" && !generic[nPub]:
				// A publisher container: flag only children that
				// carry the app's name — siblings may belong to
				// other products from the same vendor.
				for _, sub := range kidNames(p) {
					if norm(sub) == nName && !claimed(filepath.Join(p, sub), e, all) {
						out = append(out, Item{Path: filepath.Join(p, sub), Kind: KindDir, Confidence: Likely,
							Why: "app folder under the publisher's folder"})
					}
				}
			case similarName(dn, nName, first):
				if claimed(p, e, all) {
					continue
				}
				out = append(out, Item{Path: p, Kind: KindDir, Confidence: Review,
					Why: "folder name is similar — verify before removing"})
			}
		}
	}

	// Shortcuts: Start Menu and Desktop .lnk files carrying the name.
	for _, root := range shortcuts {
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if isReparse(d) {
					return filepath.SkipDir
				}
				return nil
			}
			if isReparse(d) || !strings.HasSuffix(strings.ToLower(d.Name()), ".lnk") {
				return nil
			}
			base := norm(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())))
			conf := Review
			why := "shortcut with a similar name"
			if base == nName || base == norm("uninstall "+e.Name) || base == norm(e.Name+" uninstaller") {
				conf, why = Likely, "shortcut to the app"
			} else if !similarName(base, nName, first) && !strings.Contains(base, nName) {
				return nil
			}
			out = append(out, Item{Path: p, Kind: KindDir, Confidence: conf, Why: why})
			return nil
		})
	}

	// Size every directory item after collection.
	for i := range out {
		if out[i].Kind == KindDir {
			out[i].Bytes = dirSize(out[i].Path)
		}
	}
	return out
}

// claimed reports whether a candidate dir is better explained by
// another still-installed app — shared vendor folders and overlapping
// names are never flagged.
func claimed(p string, self *Entry, all []Entry) bool {
	for i := range all {
		o := &all[i]
		if o == self || o.Name == self.Name {
			continue
		}
		if o.Location != "" && (pathWithin(p, o.Location) || pathWithin(o.Location, p)) {
			return true
		}
		if norm(o.Name) == norm(filepath.Base(p)) {
			return true
		}
	}
	return false
}

// similarName: the dir starts with the app's significant first word —
// catches "Docker" holding "Docker Desktop" data while generic words
// like "Tools" or "System" never prefix-match.
func similarName(dirNorm, nameNorm, first string) bool {
	if dirNorm == nameNorm {
		return false // exact matches are Likely, not Review
	}
	return len(first) >= 5 && !generic[first] && strings.HasPrefix(dirNorm, first)
}

// norm reduces a display name or directory name to lowercase
// alphanumerics, so "Docker Desktop" matches "DockerDesktop".
func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// generic names never produce Likely/Review matches on their own.
var generic = map[string]bool{
	"": true, "microsoft": true, "windows": true, "commonfiles": true,
	"tools": true, "tool": true, "system": true, "utilities": true,
	"data": true, "app": true, "apps": true, "software": true,
	"temp": true, "cache": true, "logs": true, "the": true,
	"intel": true, "nvidia": true, "amd": true, "google": true,
	"programs": true, "program": true, "new": true, "my": true,
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func kidNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !isReparse(e) {
			out = append(out, e.Name())
		}
	}
	return out
}

// pathWithin reports whether p is inside root (or equal).
func pathWithin(p, root string) bool {
	p = strings.ToLower(filepath.Clean(p))
	root = strings.ToLower(filepath.Clean(root))
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

// dirSize walks a tree, skipping reparse points.
func dirSize(root string) uint64 {
	var n uint64
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if isReparse(d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += uint64(info.Size())
			}
		}
		return nil
	})
	return n
}
