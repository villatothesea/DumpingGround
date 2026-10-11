package uninstall

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Confidence is how sure the scanner is that an item belongs to the
// app. Only certain items are ever reported — nothing fuzzy is listed
// for the user to adjudicate, because the tool, not the user, carries
// the responsibility for a correct list.
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
			}
			// No fuzzy or prefix matching: a leftover list must be
			// certain, not "probably this app's". Similar names are
			// shown nowhere rather than guessed at.
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
			if base != nName && base != norm("uninstall "+e.Name) && base != norm(e.Name+" uninstaller") {
				return nil
			}
			out = append(out, Item{Path: p, Kind: KindDir, Confidence: Likely, Why: "shortcut to the app"})
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

// norm reduces a display name or directory name to lowercase letters
// and digits, so "Docker Desktop" matches "DockerDesktop". Unicode
// letters survive — CJK app names must not collapse to an empty string
// (an empty match string flags *everything*).
func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
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
