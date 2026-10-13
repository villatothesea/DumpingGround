package uninstall

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/dumpingground/dumpingground/internal/disk"
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
	// ID pins a filesystem item's ancestor chain at discovery time;
	// RemoveItems verifies it before touching anything, so a junction
	// swapped in after the scan cannot redirect the deletion.
	ID *disk.PathID
}

// Registry reports whether removing this item edits the registry
// rather than the filesystem.
func (i Item) Registry() bool { return i.Kind != KindDir }

// AppNameKeys returns the normalized identity strings the orphan-data
// scan uses to recognize a claimed folder — each app's display name
// plus its install folder's basename.
func AppNameKeys(list []Entry) []string {
	seen := map[string]bool{}
	var out []string
	for i := range list {
		if n := norm(list[i].Name); n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
		if list[i].Location != "" {
			if n := norm(filepath.Base(list[i].Location)); n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// stageReporter lifts an optional progress callback into a plain
// func — scans pass it stage labels and a 0→1 fraction so the review
// dialog can show the work instead of appearing finished at once.
func stageReporter(p []func(string, float64)) func(string, float64) {
	if len(p) == 0 || p[0] == nil {
		return func(string, float64) {}
	}
	return p[0]
}

// Footprint lists an installed app's traces — its install folder,
// user-data directories, registry keys, startup entries and shortcuts.
// all is the full installed-app list, used to suppress anything that
// still belongs to another program. Read-only; nothing is removed.
func Footprint(e *Entry, all []Entry, protected []string, progress ...func(string, float64)) []Item {
	rep := stageReporter(progress)
	rep("Scanning app folders", 0)
	items := fsFootprint(e, all, appRoots(), shortcutRoots(), progress...)
	rep("Scanning the registry", 0.7)
	items = append(items, regFootprint(e, all)...)
	rep("Finishing up", 0.95)
	// Dedupe: an install folder sitting inside a scanned root would be
	// listed twice, and a registry key can be reached through two
	// views. Path + value name is the identity. Whitelisted paths are
	// dropped before they ever reach a list.
	seen := map[string]bool{}
	out := items[:0]
	for _, it := range items {
		key := strings.ToLower(it.Path) + "|" + it.RegValue
		if seen[key] {
			continue
		}
		if !it.Registry() && disk.ProtectedPath(it.Path, protected) {
			continue
		}
		seen[key] = true
		// Bind filesystem items to their ancestor chain now — removal
		// later must find the same identities or refuse.
		if it.Kind == KindDir && it.ID == nil {
			it.ID, _ = disk.BindPath(it.Path, false)
		}
		out = append(out, it)
	}
	return out
}

// fsFootprint scans filesystem locations: the install folder,
// well-known app-data roots and shortcut folders.
func fsFootprint(e *Entry, all []Entry, roots, shortcuts []string, progress ...func(string, float64)) []Item {
	rep := stageReporter(progress)
	var out []Item
	names := e.tokens(all)
	nPub := norm(e.Publisher)

	// The install folder itself — the registry said so. A
	// shortcut-inferred location stays Likely: it is a real folder
	// that app put there, but not proven to be the whole install.
	// saneLocation is belt-and-braces: entries sanitize at
	// enumeration, but a restored pending item could predate it.
	if e.Location != "" && SaneLocation(e.Location) && isDir(e.Location) && !claimed(e.Location, e, all) {
		conf, why := Confirmed, "the app's install folder, left behind"
		if e.LocationGuessed {
			conf, why = Likely, "install folder inferred from a shortcut"
		}
		out = append(out, Item{
			Path: e.Location, Kind: KindDir, Confidence: conf, Why: why,
		})
	}

	// Alias folders: products whose data directory shares nothing with
	// the display name — "Microsoft Visual Studio Code" writes to
	// "Code". The first matching rule wins so a channel-specific name
	// ("… Insiders") never claims the stable channel's folder.
	var aliasDirs []string
	if n := norm(e.Name); n != "" {
		for _, a := range dataAliases {
			if strings.Contains(n, a.match) {
				aliasDirs = a.rels
				break
			}
		}
	}

	for i, root := range roots {
		rep("Scanning "+root, 0.05+0.5*float64(i)/float64(len(roots)))
		for _, rel := range aliasDirs {
			p := filepath.Join(root, rel)
			if isDir(p) && !reparsePath(p) && !claimed(p, e, all) {
				out = append(out, Item{Path: p, Kind: KindDir, Confidence: Likely,
					Why: "the app's data folder"})
			}
		}
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
			case sharedRuntime[dn]:
				// Vendor runtimes outlive any single product — they are
				// never a leftover of the app being removed.
			case names[dn]:
				if claimed(p, e, all) {
					continue
				}
				out = append(out, Item{Path: p, Kind: KindDir, Confidence: Likely,
					Why: "data folder named after the app"})
			case dn == nPub && nPub != "" && !generic[nPub]:
				// A publisher container: flag only children that
				// carry the app's name — siblings may belong to
				// other products from the same vendor.
				for _, sub := range kidNames(p) {
					if names[norm(sub)] && !sharedRuntime[norm(sub)] && !claimed(filepath.Join(p, sub), e, all) {
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
	for i, root := range shortcuts {
		rep("Scanning "+root, 0.55+0.1*float64(i)/float64(len(shortcuts)))
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
			nameNoExt := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			base := norm(nameNoExt)
			if !names[base] && !names[norm(strings.TrimPrefix(nameNoExt, "Uninstall "))] {
				return nil
			}
			out = append(out, Item{Path: p, Kind: KindDir, Confidence: Likely, Why: "shortcut to the app"})
			return nil
		})
	}

	// Size every directory item after collection.
	for i := range out {
		if out[i].Kind == KindDir {
			out[i].Bytes = DirSize(out[i].Path)
		}
	}
	rep("Measuring folders", 0.65)
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

// nameTokens is the set of normalized names a leftover may carry and
// still be the app's own. Display names carry decorations the
// filesystem never uses — "(User)" scope tags, "版本 2.1.8" and bare
// "6.1.1" version tails — so every stripped variant joins the set.
// Exact matches only: "qodercnide" counts, "qoder" does not.
func nameTokens(name string) map[string]bool {
	out := map[string]bool{norm(name): true}
	if i := strings.Index(name, "("); i > 0 {
		out[norm(name[:i])] = true
	}
	if i := strings.Index(name, " - "); i > 0 {
		out[norm(name[:i])] = true
	}
	// Drop trailing version and channel words until a real name is
	// left: "Bitwig Studio 6.1.1" → "bitwigstudio",
	// "ACE Studio 版本 2.1.8" → "acestudio",
	// "Zed Nightly" → "zed", "Firefox Developer Edition" → "firefox".
	fields := strings.Fields(name)
	for len(fields) > 1 {
		last := fields[len(fields)-1]
		if last != "版本" && !isVersionWord(last) && !channelWords[strings.ToLower(last)] {
			break
		}
		fields = fields[:len(fields)-1]
		out[norm(strings.Join(fields, " "))] = true
	}
	delete(out, "")
	return out
}

// isVersionWord reports whether a display-name word is just a version
// ("6.1.1", "v2", "2024.1") rather than part of the product's name.
func isVersionWord(w string) bool {
	w = strings.TrimPrefix(strings.ToLower(w), "v")
	if w == "" {
		return false
	}
	digits := false
	for _, r := range w {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case r == '.' || r == 'x':
		default:
			return false
		}
	}
	return digits
}

// matchTokens is nameTokens plus the install folder's own basename —
// "Qoder CN IDE (User)" installs to a folder called "Qoder CN IDE".
// Generic tokens are dropped: an app actually called "Tools" must not
// flag every dir named "tools".
func (e *Entry) matchTokens() map[string]bool {
	names := nameTokens(e.Name)
	if e.Location != "" {
		names[norm(filepath.Base(e.Location))] = true
	}
	for t := range names {
		if generic[t] {
			delete(names, t)
		}
	}
	return names
}

// generic names never produce Likely/Review matches on their own.

// sharedTokens unions the match tokens of every installed app other
// than e. A name variant of ours that another install still claims —
// "Python 3.11.5" stripped to "python" while Python 3.12 remains —
// names shared territory, not a leftover.
func sharedTokens(e *Entry, all []Entry) map[string]bool {
	shared := map[string]bool{}
	for i := range all {
		if all[i].Key == e.Key && all[i].Name == e.Name {
			continue
		}
		for t := range all[i].matchTokens() {
			shared[t] = true
		}
	}
	return shared
}

// tokens is matchTokens with shared variants dropped. The exact full
// display name and the install folder's basename are always kept —
// they are specific to this entry. The check replaces a fixed list of
// common names: whatever collides is excluded, no roster to maintain.
func (e *Entry) tokens(all []Entry) map[string]bool {
	names := e.matchTokens()
	shared := sharedTokens(e, all)
	exact, loc := norm(e.Name), ""
	if e.Location != "" {
		loc = norm(filepath.Base(e.Location))
	}
	for t := range names {
		if shared[t] && t != exact && t != loc {
			delete(names, t)
		}
	}
	return names
}

var generic = map[string]bool{
	"": true, "microsoft": true, "windows": true, "commonfiles": true,
	"tools": true, "tool": true, "system": true, "utilities": true,
	"data": true, "app": true, "apps": true, "software": true,
	"temp": true, "cache": true, "logs": true, "the": true,
	"intel": true, "nvidia": true, "amd": true, "google": true,
	"programs": true, "program": true, "new": true, "my": true,
}

// dataAliases covers products whose data folder shares nothing with
// the display name, matched by a fragment of the normalized name. The
// first hit wins, so channel-specific rules must precede the plain
// one — "Visual Studio Code Insiders" must not claim "Code".
// Paths are relative to the user-data roots; absence costs nothing.
var dataAliases = []struct {
	match string   // normalized name fragment
	rels  []string // data folders under any app root
}{
	{"visualstudiocodeinsiders", []string{`Code - Insiders`}},
	{"visualstudiocode", []string{`Code`}},
	{"googlechrome", []string{`Google\Chrome`}},
	{"microsoftedge", []string{`Microsoft\Edge`}},
	{"wechat", []string{`Tencent\WeChat`}},
	{"firefoxnightly", []string{`Mozilla\Firefox`}},
	{"thunderbird", []string{`Mozilla\Thunderbird`}},
}

// channelWords are trailing display-name words that mark a release
// channel rather than the product itself — "Zed Nightly", "Firefox
// Developer Edition". Stripped variants join the token set so a
// channel's data folder (usually the plain name) still matches.
var channelWords = map[string]bool{
	"nightly": true, "beta": true, "alpha": true, "dev": true,
	"canary": true, "preview": true, "insider": true, "insiders": true,
	"stable": true, "release": true, "rc": true, "lts": true,
	"developer": true, "edition": true, "technology": true, "edge": true,
}

// sharedRuntime names vendor runtime containers that outlive every
// product using them — a leftover scan must never flag them, even
// when the app being removed is itself a runtime.
var sharedRuntime = map[string]bool{
	"microsoftvisualc": true, "vcredist": true,
	"vcruntimeminimum": true, "vcruntimeadditional": true,
	"dotnet": true, "netframework": true, "microsoftnet": true,
	"windowsdesktopruntime": true, "aspnetcore": true,
	"webview2": true, "edgewebview": true, "microsoftedgewebview2": true,
	"java": true, "javaruntimeenvironment": true, "openjdk": true,
	"adoptium": true, "microsoftjdk": true, "temurin": true,
	"msxml": true, "directx": true, "xnaframework": true,
}

// Personal reports whether the item holds user data or settings
// rather than disposable artifacts — such rows start unselected so
// removal is an explicit choice. Registry values (a startup pointer)
// and the app's own Uninstall record are never personal.
func (i Item) Personal() bool {
	switch i.Kind {
	case KindRegValue:
		return false
	case KindRegKey:
		p := strings.ToLower(i.Path)
		if strings.Contains(p, `\currentversion\uninstall\`) {
			return false
		}
		return strings.Contains(p, `\software\`)
	}
	return underDataRoot(i.Path) && !disposablePath(i.Path)
}

// MachineWide reports items owned by the machine rather than the
// user — HKLM registry trees and ProgramData folders. Listed, but
// never preselected: the blast radius covers every account.
func (i Item) MachineWide() bool {
	if i.Registry() {
		return strings.HasPrefix(strings.ToUpper(i.Path), "HKLM")
	}
	pd := os.Getenv("PROGRAMDATA")
	return pd != "" && pathWithin(i.Path, pd)
}

// underDataRoot reports whether p sits inside one of the app-data
// roots a footprint scan checks.
func underDataRoot(p string) bool {
	for _, r := range appRoots() {
		if pathWithin(p, r) {
			return true
		}
	}
	return false
}

// disposablePath reports whether a folder's leaf names it as
// disposable output — caches, logs, temp and dump directories.
func disposablePath(p string) bool {
	leaf := norm(filepath.Base(p))
	for _, d := range []string{"cache", "caches", "cachedata", "log", "logs",
		"temp", "tmp", "crashdumps", "crashreports", "dumps", "shadercache",
		"gpucache", "webcache", "blobstorage", "service worker"} {
		if leaf == norm(d) {
			return true
		}
	}
	return false
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

// DirSize walks a tree, skipping reparse points.
func DirSize(root string) uint64 {
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
