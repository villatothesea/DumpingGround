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
	names := e.matchTokens()
	nPub := norm(e.Publisher)

	// The Apps-list entry itself — the last thing a thorough removal
	// cleans. Only listed while the key actually exists; when the vendor
	// uninstaller already removed it, there is nothing to flag.
	if e.Key != "" && keyExists(e.hiveRoot(), keyRead|e.wowBit(), e.Key) {
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
		// Direct name matches: SOFTWARE\<App>, for every stripped
		// variant of the display name. Stripped variants can collide
		// with vendor keys other installs share — "Python 3.11.5"
		// reduces to "python", which would flag SOFTWARE\Python that
		// every other Python still uses. Only the exact full display
		// name may match those shared names.
		dExact := norm(e.Name)
		for _, sub := range mustSubKeysOrNil(h.root, h.access, h.base) {
			dn := norm(sub)
			if names[dn] && dn != nPub && (dn == dExact || !sharedSoftwareKeys[dn]) {
				out = append(out, Item{
					Path: h.name + `\` + h.base + `\` + sub,
					Kind: KindRegKey, Reg32: h.access&0x100 != 0,
					Confidence: Likely, Why: "settings key named after the app",
				})
			}
		}
		// Publisher containers: SOFTWARE\<Publisher>\<App>
		if nPub != "" && !generic[nPub] {
			pk, err := openKey(h.root, h.access, h.base+`\`+e.Publisher)
			if err == nil {
				for _, sub := range mustSubKeys(pk) {
					if names[norm(sub)] {
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
				conf, why := Likely, "startup entry named after the app"
				if e.Location != "" && pathWithin(unquote(data), filepath.Clean(e.Location)) {
					conf, why = Confirmed, "startup entry runs something in the install folder"
				} else if !names[norm(name)] {
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

// sharedSoftwareKeys are vendor-tree names so many products install
// under that a name match would flag keys belonging to other installs.
// An app whose own display name IS one of these (e.g. "Git") still
// matches its key — the exact-name check covers that.
var sharedSoftwareKeys = map[string]bool{
	"python": true, "pythoncore": true, "java": true, "oracle": true,
	"node": true, "nodejs": true, "microsoft": true, "windows": true,
	"adobe": true, "apache": true, "nginx": true, "perl": true,
	"ruby": true, "php": true, "google": true, "mozilla": true,
	"docker": true, "kubernetes": true, "vmware": true, "virtualbox": true,
	"intel": true, "nvidia": true, "amd": true, "realtek": true,
	"amazon": true, "jetbrains": true,
	"packages": true, "virtualstore": true, "classes": true,
}

// hiveRoot maps the entry's display hive back to its handle constant.
func (e *Entry) hiveRoot() uintptr {
	if e.Hive == "HKCU" {
		return hkeyCurrentUser
	}
	return hkeyLocalMachine
}

// wowBit returns the 32-bit registry view flag for this entry.
func (e *Entry) wowBit() uintptr {
	if e.Wow32 {
		return 0x100
	}
	return 0
}

// Gone reports whether the app's Uninstall entry has vanished — the
// signal that its uninstaller actually ran to completion.
func (e *Entry) Gone() bool {
	return e.Key == "" || !keyExists(e.hiveRoot(), keyRead|e.wowBit(), e.Key)
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

// mustSubKeysOrNil opens a key's child names, nil when it cannot be
// opened — used where a missing key means "nothing matches", not an
// error.
func mustSubKeysOrNil(root, access uintptr, sub string) []string {
	k, err := openKey(root, access, sub)
	if err != nil {
		return nil
	}
	defer k.close()
	return mustSubKeys(k)
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
