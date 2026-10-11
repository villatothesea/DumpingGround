//go:build windows

package uninstall

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// appRoots are the well-known per-user and per-machine locations where
// installed apps leave data the vendor uninstaller never touches.
func appRoots() []string {
	var roots []string
	for _, env := range []string{"APPDATA", "LOCALAPPDATA", "PROGRAMDATA"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, v)
		}
	}
	if l := os.Getenv("LOCALAPPDATA"); l != "" {
		roots = append(roots, filepath.Join(l, "Low"))
	}
	return roots
}

// shortcutRoots are where app shortcuts accumulate: the user's and the
// all-users Start Menu, plus the desktops.
func shortcutRoots() []string {
	var roots []string
	if v := os.Getenv("APPDATA"); v != "" {
		roots = append(roots, filepath.Join(v, `Microsoft\Windows\Start Menu\Programs`))
	}
	if v := os.Getenv("PROGRAMDATA"); v != "" {
		roots = append(roots, filepath.Join(v, `Microsoft\Windows\Start Menu\Programs`))
	}
	if v := os.Getenv("USERPROFILE"); v != "" {
		roots = append(roots, filepath.Join(v, "Desktop"), filepath.Join(v, `OneDrive\Desktop`))
	}
	if v := os.Getenv("PUBLIC"); v != "" {
		roots = append(roots, filepath.Join(v, "Desktop"))
	}
	return roots
}

// isReparse reports whether a walked entry is a symlink, junction or
// mount point — footprint scanning and sizing never follow them.
func isReparse(e os.DirEntry) bool {
	if e.Type()&os.ModeSymlink != 0 {
		return true
	}
	info, err := e.Info()
	if err != nil {
		return true // cannot inspect — do not touch
	}
	if w, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return w.FileAttributes&0x400 != 0 // FILE_ATTRIBUTE_REPARSE_POINT
	}
	return false
}

// regFootprint scans the registry for the app's own entries: the
// Uninstall subkey itself, vendor trees under Software, and Run /
// RunOnce values that point into the install folder.
func regFootprint(e *Entry) []Item {
	var out []Item
	nName := norm(e.Name)
	nPub := norm(e.Publisher)

	// The Apps-list entry itself — the last thing a thorough removal
	// cleans, and the only way to drop a broken/orphaned listing.
	if e.Key != "" {
		out = append(out, Item{
			Path: e.Hive + `\` + e.Key, Kind: KindRegKey, Reg32: e.Wow32,
			Confidence: Confirmed, Why: "the entry in the installed-apps list",
		})
	}

	type hiveRoot struct {
		name   string
		root   uintptr
		access uintptr
		base   string
	}
	hives := []hiveRoot{
		{"HKCU", hkeyCurrentUser, keyRead, `SOFTWARE`},
		{"HKLM", hkeyLocalMachine, keyRead, `SOFTWARE`},
		{"HKLM", hkeyLocalMachine, keyWow64, `SOFTWARE\WOW6432Node`},
	}
	for _, h := range hives {
		// Direct name matches: SOFTWARE\<App>
		if keyExists(h.root, h.access, h.base+`\`+e.Name) {
			out = append(out, Item{
				Path: h.name + `\` + h.base + `\` + e.Name,
				Kind: KindRegKey, Reg32: h.access&0x100 != 0,
				Confidence: Likely, Why: "settings key named after the app",
			})
		}
		// Publisher containers: SOFTWARE\<Publisher>\<App>
		if nPub != "" && !generic[nPub] {
			pk, err := openKey(h.root, h.access, h.base+`\`+e.Publisher)
			if err == nil {
				for _, sub := range mustSubKeys(pk) {
					if norm(sub) == nName {
						out = append(out, Item{
							Path: h.name + `\` + h.base + `\` + e.Publisher + `\` + sub,
							Kind: KindRegKey, Reg32: h.access&0x100 != 0,
							Confidence: Likely, Why: "app key under the publisher's key",
						})
					}
				}
				pk.close()
			}
		}
		// Startup values pointing into the install folder — dead
		// references once the app is gone.
		for _, runKey := range []string{
			h.base + `\Microsoft\Windows\CurrentVersion\Run`,
			h.base + `\Microsoft\Windows\CurrentVersion\RunOnce`,
		} {
			rk, err := openKey(h.root, h.access, runKey)
			if err != nil {
				continue
			}
			vals, _ := rk.values()
			rk.close()
			for name, v := range vals {
				data := v.str()
				conf, why := Review, "startup value named after the app"
				if e.Location != "" && pathWithin(unquote(data), filepath.Clean(e.Location)) {
					conf, why = Likely, "startup value runs something in the install folder"
				} else if norm(name) != nName {
					continue
				}
				out = append(out, Item{
					Path: h.name + `\` + runKey, RegValue: name,
					Kind: KindRegValue, Reg32: h.access&0x100 != 0,
					Confidence: conf, Why: why,
				})
			}
		}
	}
	return out
}

// keyExists reports whether a registry path opens for reading.
func keyExists(root, access uintptr, sub string) bool {
	k, err := openKey(root, access, sub)
	if err != nil {
		return false
	}
	k.close()
	return true
}

func mustSubKeys(k regKey) []string {
	names, _ := k.subKeys()
	return names
}

// unquote strips a command line's leading quote so path checks can
// compare it against the install folder.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '"' {
		if end := strings.Index(s[1:], `"`); end >= 0 {
			return s[1 : 1+end]
		}
	}
	exe, _ := SplitCmdline(s)
	return exe
}
