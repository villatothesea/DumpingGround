//go:build windows

package uninstall

import (
	"os"
	"path/filepath"
	"strings"
)

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
				if saneLocation(dir) {
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
	// The path shows up twice: 8-bit and UTF-16. UTF-16 first — it is
	// unambiguous — then the ANSI form.
	for _, off := range []int{0, 1} { // UTF-16 strings may sit at odd offsets
		if t := scanUTF16Path(data, off); t != "" {
			return t
		}
	}
	return scanANSIPath(data)
}

func scanUTF16Path(data []byte, off int) string {
	var b strings.Builder
	for i := off; i+1 < len(data); i += 2 {
		r := rune(data[i]) | rune(data[i+1])<<8
		if isPathChar(r) {
			b.WriteRune(r)
			continue
		}
		if s := absPath(b.String()); s != "" {
			return s
		}
		b.Reset()
	}
	return absPath(b.String())
}

func scanANSIPath(data []byte) string {
	var b strings.Builder
	for _, by := range data {
		if isPathChar(rune(by)) {
			b.WriteByte(by)
			continue
		}
		if s := absPath(b.String()); s != "" {
			return s
		}
		b.Reset()
	}
	return absPath(b.String())
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
