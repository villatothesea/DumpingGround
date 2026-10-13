//go:build windows

package uninstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMsiProductCode(t *testing.T) {
	e := &Entry{
		Key:       `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{ABCDEF01-2345-6789-ABCD-EF0123456789}`,
		Uninstall: `MsiExec.exe /I{ABCDEF01-2345-6789-ABCD-EF0123456789}`,
	}
	if got := msiProductCode(e); got != "{ABCDEF01-2345-6789-ABCD-EF0123456789}" {
		t.Fatalf("expected product code, got %q", got)
	}
	// A GUID in a plain exe uninstall string is not MSI.
	e = &Entry{Key: `Uninstall\SomeApp`, Uninstall: `"C:\Apps\u.exe" {ABCDEF01-2345-6789-ABCD-EF0123456789}`}
	if got := msiProductCode(e); got != "" {
		t.Fatalf("non-msiexec GUID must not qualify, got %q", got)
	}
}

func TestPlanLaunchInno(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "unins000.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe[:len(exe)-4]+".dat", []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Entry{Name: "InnoApp", Uninstall: `"` + exe + `"`}
	gotExe, tail := planLaunch(e)
	if gotExe != exe || !strings.Contains(tail, "/SILENT") || !strings.Contains(tail, "/NORESTART") {
		t.Fatalf("Inno should get silent switches, got %q %q", gotExe, tail)
	}
	// The .dat twin missing → not provably Inno → command untouched.
	if err := os.Remove(exe[:len(exe)-4] + ".dat"); err != nil {
		t.Fatal(err)
	}
	if _, tail := planLaunch(e); strings.Contains(tail, "/SILENT") {
		t.Fatal("unins000.exe without its .dat must not get silent flags")
	}
}

func TestPlanLaunchNsis(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "uninstall.exe")
	payload := append([]byte("MZ..."), []byte("Nullsoft Install System")...)
	if err := os.WriteFile(exe, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Entry{Name: "NsisApp", Uninstall: `"` + exe + `" /x=1`}
	if _, tail := planLaunch(e); !strings.Contains(tail, "/S") {
		t.Fatalf("NSIS should get /S, got %q", tail)
	}
	// The same filename without the marker stays interactive.
	if err := os.WriteFile(exe, []byte("MZ no marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, tail := planLaunch(e); strings.Contains(tail, "/S") {
		t.Fatal("an exe without the Nullsoft marker must not get /S")
	}
}

func TestPlanLaunchSteamStaysInteractive(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "uninstall.exe")
	payload := append([]byte("MZ"), []byte("Nullsoft")...)
	os.WriteFile(exe, payload, 0o644)
	os.WriteFile(filepath.Join(dir, "steam.exe"), []byte("x"), 0o644)
	e := &Entry{Name: "Steam", Uninstall: `"` + exe + `"`}
	if _, tail := planLaunch(e); strings.Contains(tail, "/S") {
		t.Fatal("Steam's uninstaller keeps its window — the games warning must show")
	}
	// A different app in the same situation still silences.
	e.Name = "NotSteam"
	if _, tail := planLaunch(e); !strings.Contains(tail, "/S") {
		t.Fatal("non-Steam NSIS should silence")
	}
}

func TestPlanLaunchQuietPreferred(t *testing.T) {
	e := &Entry{
		Name:        "Q",
		Uninstall:   `"C:\a\u.exe"`,
		QuietUninst: `"C:\a\u.exe" /quiet`,
	}
	if _, tail := planLaunch(e); tail != "/quiet" {
		t.Fatalf("the registered quiet command wins, got %q", tail)
	}
}

func TestPlanLaunchMsi(t *testing.T) {
	e := &Entry{
		Name:      "MsiApp",
		Key:       `Uninstall\{12345678-1234-1234-1234-123456789012}`,
		Uninstall: `MsiExec.exe /I{12345678-1234-1234-1234-123456789012}`,
	}
	exe, tail := planLaunch(e)
	if !strings.HasSuffix(strings.ToLower(exe), "msiexec.exe") {
		t.Fatalf("MSI must go through msiexec, got %q", exe)
	}
	if !strings.Contains(tail, "/x {12345678-1234-1234-1234-123456789012}") ||
		!strings.Contains(tail, "/passive") || !strings.Contains(tail, "ReallySuppress") {
		t.Fatalf("MSI args wrong: %q", tail)
	}
}
