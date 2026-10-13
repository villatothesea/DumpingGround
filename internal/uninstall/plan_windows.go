//go:build windows

package uninstall

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// planLaunch picks how an entry's uninstaller runs:
//
//  1. an MSI product code → msiexec /x {GUID} /passive — deterministic,
//     no bootstrapper guessing, progress UI but no prompts, no reboot;
//  2. the vendor's registered QuietUninstallString, verbatim;
//  3. a recognized uninstaller (Inno Setup, NSIS) with its silent
//     switches added — recognized by file signature, never by name;
//  4. the registered command, untouched.
//
// Silent augments are added only for installers whose silent switch is
// documented and uniform; anything unrecognized gets its own UI, which
// is itself a confirmation layer.
func planLaunch(e *Entry) (exe, tail string) {
	if code := msiProductCode(e); code != "" {
		if exe = msiexecPath(); exe != "" {
			return exe, `/x ` + code + ` /passive REBOOT=ReallySuppress /norestart`
		}
	}
	if exe, tail = splitExeTail(e.QuietUninst); exe != "" {
		return exe, tail
	}
	exe, tail = splitExeTail(e.Uninstall)
	if exe == "" {
		return "", ""
	}
	switch {
	case isInnoUninstaller(exe):
		if !uninstallTailSilent(tail) {
			tail = strings.TrimSpace(tail + " /SILENT /SUPPRESSMSGBOXES /NORESTART")
		}
	case isNsisUninstaller(exe) && !isSteamClientUninstaller(e, exe):
		if !uninstallTailSilent(tail) {
			tail = strings.TrimSpace(tail + " /S")
		}
	}
	return exe, tail
}

var guidRe = regexp.MustCompile(`\{[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}\}`)

// msiProductCode extracts the Windows Installer product code. Entries
// keyed by a GUID are MSI products, and a plain msiexec command with a
// GUID in it qualifies too. A GUID key name alone is not proof — some
// vendors key on GUIDs while their uninstaller is a custom exe, so the
// registered command must either mention msiexec, repeat the code, or
// be empty before msiexec is planned.
func msiProductCode(e *Entry) string {
	keyName := e.Key
	if i := strings.LastIndexByte(keyName, '\\'); i >= 0 {
		keyName = keyName[i+1:]
	}
	if code := guidRe.FindString(keyName); code != "" {
		l := strings.ToLower(e.Uninstall)
		if l == "" || strings.Contains(l, "msiexec") ||
			strings.Contains(strings.ToUpper(l), strings.ToUpper(code)) {
			return strings.ToUpper(code)
		}
		return ""
	}
	if strings.Contains(strings.ToLower(e.Uninstall), "msiexec") {
		if code := guidRe.FindString(e.Uninstall); code != "" {
			return strings.ToUpper(code)
		}
	}
	return ""
}

// msiexecPath resolves the system msiexec — never a PATH lookalike.
func msiexecPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, `System32\msiexec.exe`)
}

var innoUninsRe = regexp.MustCompile(`(?i)^unins\d{3}\.exe$`)

// isInnoUninstaller recognizes Inno Setup: uninsNNN.exe only counts
// when its uninsNNN.dat sits beside it — the data file is the setup's
// own uninstall record, so its presence proves the pair.
func isInnoUninstaller(exe string) bool {
	if !innoUninsRe.MatchString(filepath.Base(exe)) {
		return false
	}
	dat := exe[:len(exe)-4] + ".dat"
	_, err := os.Stat(dat)
	return err == nil
}

// isNsisUninstaller recognizes NSIS by the "Nullsoft" marker in the
// uninstaller's first megabyte — never by name, since other
// uninstall.exe programs may read /S as something else.
func isNsisUninstaller(exe string) bool {
	if !strings.HasSuffix(strings.ToLower(exe), ".exe") {
		return false
	}
	f, err := os.Open(exe)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 1<<20)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), "Nullsoft")
}

// isSteamClientUninstaller keeps Steam's NSIS uninstaller interactive:
// Valve's window warns that the installed games go with it, and that
// warning must not be silenced away.
func isSteamClientUninstaller(e *Entry, exe string) bool {
	if !strings.EqualFold(e.Name, "Steam") {
		return false
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(exe), "steam.exe"))
	return err == nil
}

// uninstallTailSilent reports whether the argument tail already asks
// for a quiet run — /S, /SILENT, /VERYSILENT, /quiet, /passive, /q.
func uninstallTailSilent(tail string) bool {
	for _, f := range strings.Fields(strings.ToLower(tail)) {
		switch strings.TrimLeft(f, "/-") {
		case "s", "silent", "verysilent", "quiet", "passive", "q", "qn":
			return true
		}
	}
	return false
}
