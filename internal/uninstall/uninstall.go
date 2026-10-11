// Package uninstall enumerates installed software from the registry's
// standard Uninstall keys — the same source Settings > Apps reads.
package uninstall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Entry is one installed program. SizeBytes comes from the registry's
// EstimatedSize (kilobytes); zero when the entry does not report one.
// UninstallString is the command line Windows itself would run — we
// launch it as-is rather than removing anything ourselves.
type Entry struct {
	Name          string
	Version       string
	Publisher     string
	SizeBytes     uint64
	InstallDate   string // YYYYMMDD when present, formatted at display time
	Location      string
	Uninstall     string
	QuietUninst   string
	Icon          string
	SystemScope   bool // machine-wide (HKLM) vs current-user (HKCU)
	Key           string
	Hive          string // "HKLM" or "HKCU" — the display root of Key
	Wow32         bool   // Key lives in the 32-bit (WOW6432Node) view
	EstimatedSize bool   // false when the app never reported a size
}

// List returns every installed program with a name and an uninstall
// command, merging the three hives Windows itself consults. Entries
// flagged SystemComponent — runtime dependencies and OS patches — are
// left out, matching the Apps settings page.
func List() []Entry {
	var out []Entry
	for _, e := range enumUninstall() {
		if e.Name == "" || e.Uninstall == "" {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SizeBytes != out[j].SizeBytes {
			return out[i].SizeBytes > out[j].SizeBytes
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Date renders the registry's YYYYMMDD InstallDate as YYYY-MM-DD.
// Installers also write free-form dates ("2023.10.05") — those pass
// through untouched rather than being dropped.
func (e *Entry) Date() string {
	s := e.InstallDate
	if len(s) == 8 && isDigits(s) {
		return s[0:4] + "-" + s[4:6] + "-" + s[6:8]
	}
	return s
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Launch runs the entry's uninstall command line and returns the
// started process so the caller can wait for the uninstaller to exit.
// Windows stores the string free-form, so it is split back into exe
// and arguments here; the uninstaller's own UI then takes over.
func (e *Entry) Launch() (*exec.Cmd, error) {
	exe, args := SplitCmdline(e.Uninstall)
	if exe == "" {
		return nil, fmt.Errorf("no uninstall command")
	}
	cmd := exec.Command(exe, args...)
	hideWindow(cmd) // console-subsystem uninstallers would flash a cmd window
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// regPath cleans a registry path value — strips the wrapping quotes
// many installers include and expands %ENV% variables.
func regPath(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`)
	return os.ExpandEnv(s)
}

// IconPath resolves a file the shell can raster an icon from. It
// prefers DisplayIcon — unquoted, ",N" index stripped, env-expanded —
// and falls back to the app's exe inside InstallLocation when the
// registry has no icon or points at the uninstaller itself.
func (e *Entry) IconPath() string {
	if s := e.displayIconPath(); s != "" && !isUninstallerName(s) {
		return s
	}
	return e.exeFallback()
}

// displayIconPath cleans the registry DisplayIcon value.
func (e *Entry) displayIconPath() string {
	s := regPath(e.Icon)
	if i := strings.LastIndex(s, ","); i > 0 {
		if _, err := strconv.Atoi(strings.TrimSpace(s[i+1:])); err == nil {
			s = s[:i]
		}
	}
	if s == "" || strings.ContainsAny(s, "<>|?*") {
		return ""
	}
	if _, err := os.Stat(s); err != nil {
		return ""
	}
	return s
}

// exeFallback looks inside the install folder for a plausible app
// binary: first a name match, then "the only real exe here".
// Uninstallers and updaters are never picked.
func (e *Entry) exeFallback() string {
	if e.Location == "" {
		return ""
	}
	names := e.matchTokens()
	var only string
	for _, dir := range []string{e.Location, filepath.Join(e.Location, "bin")} {
		kids, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		count := 0
		for _, k := range kids {
			if k.IsDir() || !isIconBinary(k.Name()) {
				continue
			}
			p := filepath.Join(dir, k.Name())
			if isUninstallerName(p) {
				continue
			}
			count++
			stem := norm(strings.TrimSuffix(k.Name(), filepath.Ext(k.Name())))
			if names[stem] {
				return p // name match wins outright
			}
			only = p
		}
		if count == 1 {
			return only
		}
	}
	return ""
}

// isIconBinary reports whether a filename can carry an embedded icon —
// plugins ship as .vst3/.dll, not .exe.
func isIconBinary(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".exe", ".dll", ".vst3", ".ocx":
		return true
	}
	return false
}

// locationFromUninstall guesses the install dir from the uninstall
// command's own path when the registry leaves InstallLocation empty —
// Inno/NSIS per-user installs put their uninstaller next to the app.
// System executables (msiexec, rundll32) give up: their directory is
// Windows, not the app.
func (e *Entry) locationFromUninstall() string {
	exe, _ := SplitCmdline(e.Uninstall)
	if exe == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(exe))
	if base == "msiexec.exe" || base == "rundll32.exe" || base == "rundll32" || base == "cmd.exe" || base == "regsvr32.exe" {
		return ""
	}
	dir := filepath.Dir(exe)
	if dir == "." || dir == "" {
		return ""
	}
	if windir := os.Getenv("WINDIR"); windir != "" && pathWithin(dir, windir) {
		return ""
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// locationFallback tries the uninstall string first, then the icon's
// own directory — DisplayIcon usually points at the app exe itself.
func (e *Entry) locationFallback() string {
	if d := e.locationFromUninstall(); d != "" {
		return d
	}
	if s := e.displayIconPath(); s != "" {
		dir := filepath.Dir(s)
		if windir := os.Getenv("WINDIR"); windir == "" || !pathWithin(dir, windir) {
			if isUninstallerName(s) == false {
				return dir
			}
		}
	}
	return ""
}

// saneLocation rejects registry locations that name a well-known root
// rather than the app's own folder — some installers write
// "C:\Program Files\" itself, and flagging that as a leftover would
// be catastrophic.
func saneLocation(p string) bool {
	if p == "" {
		return false
	}
	c := filepath.Clean(p)
	if filepath.Dir(c) == c {
		return false // a drive root is never an install folder
	}
	for _, env := range []string{
		"PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMW6432",
		"COMMONPROGRAMFILES", "COMMONPROGRAMFILES(X86)", "COMMONPROGRAMW6432",
		"PROGRAMDATA", "APPDATA", "LOCALAPPDATA", "USERPROFILE", "WINDIR", "PUBLIC",
	} {
		if v := os.Getenv(env); v != "" && strings.EqualFold(filepath.Clean(v), c) {
			return false
		}
	}
	return true
}

// programFilesLocation is the last resort for entries whose registry
// record knows nothing: a top-level folder in Program Files named
// exactly after the app ("Node.js" → "nodejs") is near-certainly it.
func (e *Entry) programFilesLocation() string {
	names := e.matchTokens()
	seen := map[string]bool{}
	var roots []string
	for _, env := range []string{"PROGRAMFILES", "PROGRAMW6432", "PROGRAMFILES(X86)"} {
		if v := os.Getenv(env); v != "" && !seen[strings.ToLower(v)] {
			seen[strings.ToLower(v)] = true
			roots = append(roots, v)
		}
	}
	if l := os.Getenv("LOCALAPPDATA"); l != "" {
		roots = append(roots, filepath.Join(l, "Programs"))
	}
	for _, root := range roots {
		kids, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, k := range kids {
			if k.IsDir() && names[norm(k.Name())] {
				return filepath.Join(root, k.Name())
			}
		}
	}
	return ""
}

// isUninstallerName reports whether a path names an uninstaller,
// updater or setup stub — never a good app icon.
func isUninstallerName(path string) bool {
	stem := norm(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	for _, bad := range []string{"unins", "uninstall", "unwise", "setup", "update", "install"} {
		if strings.HasPrefix(stem, bad) {
			return true
		}
	}
	return false
}

// SplitCmdline breaks a stored command line into exe and arguments.
// Uninstall strings come in three shapes: `"C:\a b\u.exe" /x`,
// `C:\a\u.exe /x` and `MsiExec.exe /I{GUID}`.
func SplitCmdline(s string) (string, []string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if s[0] == '"' {
		if end := strings.Index(s[1:], `"`); end >= 0 {
			return s[1 : 1+end], splitArgs(s[1+end+1:])
		}
		return strings.Trim(s, `"`), nil
	}
	if i := strings.Index(strings.ToLower(s), ".exe"); i >= 0 {
		return s[:i+4], splitArgs(s[i+4:])
	}
	f := strings.Fields(s)
	return f[0], splitArgs(s[len(f[0]):])
}

// splitArgs splits an argument tail on spaces, honoring double quotes.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
