//go:build windows

package uninstall

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf16"
)

// classRoot is one Classes hive view plus its subkey names, enumerated
// once — the ProgID list is thousands of entries and every
// location-less app asks the same question.
type classRoot struct {
	root, access uintptr
	sub          string
	names        []string
}

var (
	classRootsOnce sync.Once
	classRoots     []classRoot
)

func classesRoots() []classRoot {
	classRootsOnce.Do(func() {
		for _, h := range []struct {
			root, access uintptr
			sub          string
		}{
			{hkeyLocalMachine, keyRead, `SOFTWARE\Classes`},
			{hkeyCurrentUser, keyRead, `SOFTWARE\Classes`},
			{hkeyLocalMachine, keyRead, `SOFTWARE\WOW6432Node\Classes`},
		} {
			k, err := openKey(h.root, h.access, h.sub)
			if err != nil {
				continue
			}
			classRoots = append(classRoots, classRoot{h.root, h.access, h.sub, mustSubKeys(k)})
			k.close()
		}
	})
	return classRoots
}

// assocLocation infers the install dir from the app's registered
// file associations — a ProgID like "BitwigStudio.bwproject" has a
// shell\open\command (and often DefaultIcon) containing the exe path.
// Links that store an AppUserModelID instead of a path resolve this
// way; PackagedApps-style entries do too. The result is a guess:
// footprint lists it as Likely, never Confirmed.
func (e *Entry) assocLocation() (dir, iconPath string) {
	names := e.matchTokens()
	matches := func(progID string) bool {
		for _, seg := range strings.Split(progID, ".") {
			if names[norm(seg)] {
				return true
			}
		}
		return false
	}
	for _, h := range classesRoots() {
		for _, id := range h.names {
			if !matches(id) {
				continue
			}
			// shell\open\command gives the exe; DefaultIcon gives an
			// icon when the command carries none.
			for _, leaf := range []string{`shell\open\command`, "DefaultIcon"} {
				k, err := openKey(h.root, h.access, h.sub+`\`+id+`\`+leaf)
				if err != nil {
					continue
				}
				vals, _ := k.values()
				k.close()
				raw := vals[""].str()
				if raw == "" {
					continue
				}
				exe, _ := SplitCmdline(raw)
				if leaf == "DefaultIcon" {
					exe = cleanIconValue(raw)
				}
				if exe == "" {
					continue
				}
				if leaf == "DefaultIcon" && iconPath == "" {
					iconPath = exe
				}
				if d := filepath.Dir(exe); dir == "" && SaneLocation(d) {
					dir = d
				}
			}
			if dir != "" && iconPath != "" {
				break
			}
		}
		if dir != "" {
			break
		}
	}
	return dir, iconPath
}

// appPathsLocation checks HKLM\...\App Paths\<name>.exe — the shell's
// "where is this app" table. Cheap and authoritative when present.
func (e *Entry) appPathsLocation() string {
	names := e.matchTokens()
	for _, h := range []struct {
		root uintptr
		sub  string
	}{
		{hkeyLocalMachine, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`},
		{hkeyCurrentUser, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`},
	} {
		ap, err := openKey(h.root, keyRead, h.sub)
		if err != nil {
			continue
		}
		for _, sub := range mustSubKeys(ap) {
			stem := norm(strings.TrimSuffix(sub, filepath.Ext(sub)))
			if !names[stem] {
				continue
			}
			k, err := openKey(h.root, keyRead, h.sub+`\`+sub)
			if err != nil {
				continue
			}
			vals, _ := k.values()
			k.close()
			if exe := vals[""].str(); exe != "" {
				if d := filepath.Dir(regPath(exe)); SaneLocation(d) {
					ap.close()
					return d
				}
			}
		}
		ap.close()
	}
	return ""
}

// shortcutLocation infers the install dir from a Start Menu or
// Desktop shortcut named after the app — the last resort for entries
// whose registry record knows nothing (custom-path MSI installs like
// Bitwig). The link's target exe normally sits at the install root or
// one level down in bin/, so both are tried. The result is marked a
// guess: footprint treats it as Likely, never Confirmed.
func (e *Entry) shortcutLocation() string {
	names := e.matchTokens()
	for _, root := range shortcutRoots() {
		var found string
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if found != "" || err != nil {
				return nil
			}
			if d.IsDir() {
				if isReparse(d) {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if isReparse(d) || !strings.HasSuffix(strings.ToLower(name), ".lnk") {
				return nil
			}
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			if !names[norm(stem)] && !names[norm(strings.TrimPrefix(stem, "Uninstall "))] {
				return nil
			}
			if target := linkTarget(p); target != "" {
				dir := filepath.Dir(target)
				if SaneLocation(dir) {
					found = dir
				}
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// linkTarget extracts the target path from a .lnk file without COM:
// LinkInfo stores the local base path as text, so scanning the bytes
// for a drive-letter run is enough. Read-only.
func linkTarget(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return ""
	}
	return scanLink(data, absPath)
}

// linkTargetRaw is the link's true target whether it still exists or
// not — the orphan scan wants precisely the dead ones. It parses the
// LinkInfo structure rather than scanning bytes: byte scans misread
// ANSI strings as UTF-16 at odd offsets and invent path-shaped runs.
func linkTargetRaw(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return ""
	}
	return parseLinkTarget(data)
}

// parseLinkTarget reads the LinkInfo local base path — the shortcut's
// recorded target — from the shell-link binary layout (MS-SHLLINK).
// "" when the link stores none (AppUserModelID links, network-only
// links): nothing was proven about any target then.
func parseLinkTarget(data []byte) string {
	if len(data) < 0x4C || binary.LittleEndian.Uint32(data) != 0x4C {
		return ""
	}
	flags := binary.LittleEndian.Uint32(data[0x14:])
	off := 0x4C
	if flags&0x1 != 0 { // HasLinkTargetIDList — skip the PIDL block
		if off+2 > len(data) {
			return ""
		}
		off += 2 + int(binary.LittleEndian.Uint16(data[off:]))
	}
	if flags&0x2 == 0 || off+0x1C > len(data) { // no LinkInfo
		return ""
	}
	liSize := int(binary.LittleEndian.Uint32(data[off:]))
	if liSize < 0x1C || off+liSize > len(data) {
		return ""
	}
	li := data[off : off+liSize]
	if binary.LittleEndian.Uint32(li[8:])&0x1 == 0 {
		return "" // no local base path — network-linked only
	}
	hdr := int(binary.LittleEndian.Uint32(li[4:]))
	if hdr >= 0x24 { // newer headers carry a UTF-16 local base path
		if uoff := int(binary.LittleEndian.Uint32(li[0x1C:])); uoff > 0 && uoff < liSize {
			if s := utf16StringAt(li[uoff:]); s != "" {
				return s
			}
		}
	}
	if aoff := int(binary.LittleEndian.Uint32(li[0x10:])); aoff > 0 && aoff < liSize {
		return ansiStringAt(li[aoff:])
	}
	return ""
}

// utf16StringAt reads a NUL-terminated UTF-16 string at the start of b.
func utf16StringAt(b []byte) string {
	var u []uint16
	for i := 0; i+1 < len(b); i += 2 {
		r := binary.LittleEndian.Uint16(b[i:])
		if r == 0 {
			break
		}
		u = append(u, r)
	}
	return strings.TrimSpace(string(utf16.Decode(u)))
}

// ansiStringAt reads a NUL-terminated byte string at the start of b.
func ansiStringAt(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		return strings.TrimSpace(string(b[:i]))
	}
	return strings.TrimSpace(string(b))
}

func scanLink(data []byte, valid func(string) string) string {
	// The path shows up twice: 8-bit and UTF-16. UTF-16 first — it is
	// unambiguous — then the ANSI form.
	for _, off := range []int{0, 1} { // UTF-16 strings may sit at odd offsets
		if t := scanUTF16Path(data, off, valid); t != "" {
			return t
		}
	}
	return scanANSIPath(data, valid)
}

func scanUTF16Path(data []byte, off int, valid func(string) string) string {
	var b strings.Builder
	for i := off; i+1 < len(data); i += 2 {
		r := rune(data[i]) | rune(data[i+1])<<8
		if isPathChar(r) {
			b.WriteRune(r)
			continue
		}
		if s := valid(b.String()); s != "" {
			return s
		}
		b.Reset()
	}
	return valid(b.String())
}

func scanANSIPath(data []byte, valid func(string) string) string {
	var b strings.Builder
	for _, by := range data {
		if isPathChar(rune(by)) {
			b.WriteByte(by)
			continue
		}
		if s := valid(b.String()); s != "" {
			return s
		}
		b.Reset()
	}
	return valid(b.String())
}

// isPathChar is permissive on purpose — spaces, CJK, punctuation all
// appear in paths; the run ends at NULs and control bytes.
func isPathChar(r rune) bool {
	return r >= 0x20 && r != 0x7F && r != '|' && r != '<' && r != '>'
}

// absPath reports whether a scanned run looks like a filesystem path —
// a drive letter, a backslash — and filters out the link's metadata
// runs by requiring it to exist on disk. Returns the expanded path.
func absPath(s string) string {
	if strings.Contains(s, "%") {
		s = os.ExpandEnv(s) // links store %VARS% verbatim
	}
	if len(s) < 4 || s[1] != ':' || s[2] != '\\' {
		return ""
	}
	if c := s[0]; !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
		return ""
	}
	if _, err := os.Stat(s); err != nil {
		return "" // the shortcut may point at something long gone
	}
	return s
}
