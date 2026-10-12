//go:build windows

package uninstall

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var procExpandEnvStr = syscall.NewLazyDLL("kernel32.dll").NewProc("ExpandEnvironmentStringsW")

// Orphans finds leftovers of software that is already gone: dead
// references (shortcuts, startup entries, App Paths and file
// associations pointing at files that no longer exist), installed-apps
// entries whose uninstaller vanished, empty Start Menu groups, and
// name-matched traces of everything this tool has uninstalled before.
// Every item is certain — a reference is dead only when its target is
// verified missing, an empty folder carries nothing to lose.
func Orphans(installed []Entry, progress ...func(string, float64)) []Item {
	rep := stageReporter(progress)
	var out []Item
	rep("Checking leftover installed-apps entries", 0)
	out = append(out, deadEntryItems(installed)...)
	rep("Checking shortcuts", 1.0/7)
	out = append(out, deadShortcutItems(shortcutRoots())...)
	rep("Checking startup entries", 2.0/7)
	out = append(out, deadRunItems()...)
	rep("Checking App Paths", 3.0/7)
	out = append(out, deadAppPathsItems()...)
	rep("Checking file associations", 4.0/7)
	out = append(out, deadAssocItems()...)
	rep("Checking Start Menu groups", 5.0/7)
	out = append(out, emptyGroupItems(startMenuRoots())...)
	rep("Checking uninstall history", 6.0/7)
	out = append(out, historyItems(installed)...)
	rep("Finishing up", 0.98)
	seen := map[string]bool{}
	items := out[:0]
	for _, it := range out {
		key := strings.ToLower(it.Path) + "|" + it.RegValue
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, it)
	}
	return items
}

// startMenuRoots are the two program-group roots — the only places an
// empty directory is provably a leftover shell. (An empty folder on
// the user's desktop is their own business.)
func startMenuRoots() []string {
	var roots []string
	if v := os.Getenv("APPDATA"); v != "" {
		roots = append(roots, filepath.Join(v, `Microsoft\Windows\Start Menu\Programs`))
	}
	if v := os.Getenv("PROGRAMDATA"); v != "" {
		roots = append(roots, filepath.Join(v, `Microsoft\Windows\Start Menu\Programs`))
	}
	return roots
}

// deadEntryItems flags registry entries still listed as installed
// whose uninstaller file no longer exists — the classic half-removed
// state: the entry sits in Apps forever, its Uninstall button can only
// fail.
func deadEntryItems(installed []Entry) []Item {
	var out []Item
	for _, e := range installed {
		if e.Key == "" {
			continue
		}
		if missingTarget(e.Uninstall) == "" {
			continue
		}
		out = append(out, Item{
			Path: e.Hive + `\` + e.Key, Kind: KindRegKey, Reg32: e.Wow32,
			Confidence: Confirmed,
			Why:        `installed-apps entry for "` + e.Name + `" — its uninstaller is gone`,
		})
	}
	return out
}

// deadShortcutItems flags .lnk files Windows itself can no longer
// resolve — the recorded target is gone and IShellLink::Resolve's own
// search fails. Links storing only an AppUserModelID have no path to
// verify and are never flagged.
func deadShortcutItems(roots []string) []Item {
	res := newLinkResolver()
	defer res.close()
	var out []Item
	for _, root := range roots {
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
			if res.linkDead(p) {
				out = append(out, Item{Path: p, Kind: KindDir,
					Confidence: Confirmed,
					Why:        "shortcut to a file that no longer exists"})
			}
			return nil
		})
	}
	return out
}

// deadRunItems flags Run/RunOnce values whose target is gone — a dead
// autostart is a leftover by definition.
func deadRunItems() []Item {
	var out []Item
	hives := []struct {
		name         string
		root, access uintptr
		base         string
	}{
		{"HKCU", hkeyCurrentUser, keyRead, `SOFTWARE`},
		{"HKLM", hkeyLocalMachine, keyRead, `SOFTWARE`},
		{"HKLM", hkeyLocalMachine, keyWow64, `SOFTWARE\WOW6432Node`},
	}
	for _, h := range hives {
		for _, leaf := range []string{
			`Microsoft\Windows\CurrentVersion\Run`,
			`Microsoft\Windows\CurrentVersion\RunOnce`,
		} {
			sub := h.base + `\` + leaf
			vals := mustValuesOrNil(h.root, h.access, sub)
			for name, v := range vals {
				if missingTarget(v.str()) == "" {
					continue
				}
				out = append(out, Item{
					Path: h.name + `\` + sub, RegValue: name,
					Kind: KindRegValue, Reg32: h.access&0x100 != 0,
					Confidence: Confirmed,
					Why:        "startup entry " + name + " points to a missing file",
				})
			}
		}
	}
	return out
}

// deadAppPathsItems flags App Paths registrations whose exe is gone.
func deadAppPathsItems() []Item {
	var out []Item
	for _, h := range []struct {
		name string
		root uintptr
		sub  string
	}{
		{"HKLM", hkeyLocalMachine, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`},
		{"HKCU", hkeyCurrentUser, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`},
	} {
		for _, sub := range mustSubKeysOrNil(h.root, keyRead, h.sub) {
			vals := mustValuesOrNil(h.root, keyRead, h.sub+`\`+sub)
			if missingTarget(vals[""].str()) == "" {
				continue
			}
			out = append(out, Item{
				Path: h.name + `\` + h.sub + `\` + sub,
				Kind: KindRegKey, Confidence: Confirmed,
				Why: "App Paths entry points to a missing program",
			})
		}
	}
	return out
}

// deadAssocItems flags file-association ProgIDs whose open command
// runs a missing exe. Only extensions a user could still double-click
// are resolved — every `.<ext>` key's default ProgID — so orphaned
// helper registrations never listed by an extension stay unreported.
func deadAssocItems() []Item {
	var out []Item
	seen := map[string]bool{}
	for _, h := range classesRoots() {
		for _, id := range h.names {
			if !strings.HasPrefix(id, ".") {
				continue
			}
			vals := mustValuesOrNil(h.root, h.access, h.sub+`\`+id)
			progID := vals[""].str()
			if progID == "" || seen[strings.ToLower(progID)] {
				continue
			}
			seen[strings.ToLower(progID)] = true
			cmd := mustValuesOrNil(h.root, h.access,
				h.sub+`\`+progID+`\shell\open\command`)
			if missingTarget(cmd[""].str()) == "" {
				continue
			}
			hive := "HKLM"
			if h.root == hkeyCurrentUser {
				hive = "HKCU"
			}
			out = append(out, Item{
				Path: hive + `\` + h.sub + `\` + progID,
				Kind: KindRegKey, Reg32: h.access&0x100 != 0,
				Confidence: Confirmed,
				Why:        "file association for " + id + " runs a missing program",
			})
		}
	}
	return out
}

// emptyGroupItems flags Start Menu program groups holding nothing —
// the uninstaller removed the shortcuts but left the folder. Windows
// keeps a few empty groups of its own (Startup must exist); those are
// on a fixed list and never flagged. A group whose only content is
// more empty groups flags its topmost ancestor — removing the leaf
// would just leave an empty parent for the next scan.
func emptyGroupItems(roots []string) []Item {
	var out []Item
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || p == root || !d.IsDir() || isReparse(d) {
				return nil
			}
			if systemGroups[norm(d.Name())] {
				return nil
			}
			if dirTreeEmpty(p) {
				out = append(out, Item{Path: p, Kind: KindDir,
					Confidence: Confirmed,
					Why:        "empty Start Menu group left behind"})
				return filepath.SkipDir
			}
			return nil
		})
	}
	return out
}

// dirTreeEmpty reports whether a directory's whole subtree holds
// nothing but empty directories and desktop.ini files. Reparse points
// count as content — never open one to check.
func dirTreeEmpty(p string) bool {
	kids, err := os.ReadDir(p)
	if err != nil {
		return false
	}
	for _, k := range kids {
		switch {
		case k.IsDir():
			if isReparse(k) || !dirTreeEmpty(filepath.Join(p, k.Name())) {
				return false
			}
		case !strings.EqualFold(k.Name(), "desktop.ini"):
			return false
		}
	}
	return true
}

// systemGroups are Start Menu groups Windows itself may leave empty.
var systemGroups = map[string]bool{
	"startup": true, "administrativetools": true, "accessories": true,
	"accessibility": true, "systemtools": true, "maintenance": true,
	"windowsaccessories": true, "windowspowershell": true,
}

// historyItems re-scans, by name, every app recorded as uninstalled —
// or recovered from past run logs. The registry entry is gone, so this
// is nameTokens-only matching against data roots and shortcuts.
func historyItems(installed []Entry) []Item {
	var out []Item
	for _, name := range HistoryNames() {
		if StillInstalled(name, installed) {
			continue
		}
		e := Entry{Name: name}
		for _, it := range Footprint(&e, installed) {
			it.Why = name + ": " + it.Why
			out = append(out, it)
		}
	}
	return out
}

// StillInstalled reports whether a name matches an app still listed
// as installed — nothing to sweep while it is.
func StillInstalled(name string, installed []Entry) bool {
	names := nameTokens(name)
	for i := range installed {
		for t := range installed[i].matchTokens() {
			if names[t] {
				return true
			}
		}
	}
	return false
}

// missingTarget extracts the first absolute path from a stored command
// or value and returns it only when the file no longer exists. For a
// launcher the subject is its argument, not the runner itself —
// `rundll32 "C:\a\b.dll",Entry` is about the DLL. For a plain exe the
// exe itself is the subject: `app.exe "arg.txt"` stays alive even when
// the argument file is gone. "" means "alive or cannot be judged".
func missingTarget(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	exe, _ := SplitCmdline(s)
	if launcherNames[strings.ToLower(filepath.Base(exe))] {
		if i := strings.IndexByte(s, '"'); i >= 0 {
			if j := strings.IndexByte(s[i+1:], '"'); j > 0 {
				if p := missingPath(s[i+1 : i+1+j]); p != "" {
					return p
				}
			}
		}
	}
	return missingPath(exe)
}

// launcherNames are command runners whose real payload sits in an
// argument, not in the exe — for these the quoted argument is what
// must exist for the entry to be alive.
var launcherNames = map[string]bool{
	"rundll32.exe": true, "rundll32": true, "regsvr32.exe": true,
	"msiexec.exe": true, "cmd.exe": true, "powershell.exe": true,
	"wscript.exe": true, "cscript.exe": true, "explorer.exe": true,
}

// missingPath returns p only when it is provably gone: a drive-letter
// path on a drive that answers, and a stat that reports ErrNotExist.
// An offline share, an unmounted encrypted volume or a permission
// error all fail here — unknown is not dead.
func missingPath(p string) string {
	p = expandEnvWin(strings.TrimSpace(strings.Trim(p, `"`)))
	if len(p) < 4 || p[1] != ':' || p[2] != '\\' {
		return ""
	}
	if c := p[0]; !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
		return ""
	}
	if _, err := os.Stat(p[:3]); err != nil {
		return "" // drive offline, unmapped or unreachable — cannot judge
	}
	_, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return p
	}
	return "" // exists — or we cannot prove otherwise
}

// expandEnvWin expands %VAR% via the Windows API — unlike os.ExpandEnv
// it leaves $ characters alone, so "C:\$Recycle.Bin\x" survives.
func expandEnvWin(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	src, err := syscall.UTF16FromString(s)
	if err != nil {
		return s
	}
	buf := make([]uint16, 4096)
	n, _, _ := procExpandEnvStr.Call(
		uintptr(unsafe.Pointer(&src[0])),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)))
	if n == 0 || n > uintptr(len(buf)) {
		return s
	}
	return syscall.UTF16ToString(buf[:n])
}

// mustValuesOrNil opens a key's values, nil when it cannot be opened.
func mustValuesOrNil(root, access uintptr, sub string) map[string]regValue {
	k, err := openKey(root, access, sub)
	if err != nil {
		return nil
	}
	defer k.close()
	vals, _ := k.values()
	return vals
}
