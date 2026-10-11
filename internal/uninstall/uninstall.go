// Package uninstall enumerates installed software from the registry's
// standard Uninstall keys — the same source Settings > Apps reads.
package uninstall

import (
	"fmt"
	"os/exec"
	"sort"
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
	EstimatedSize bool // false when the app never reported a size
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
func (e *Entry) Date() string {
	if len(e.InstallDate) != 8 {
		return ""
	}
	return e.InstallDate[0:4] + "-" + e.InstallDate[4:6] + "-" + e.InstallDate[6:8]
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
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
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
