package uninstall

import (
	"slices"
	"testing"
)

func TestSplitCmdline(t *testing.T) {
	for _, tc := range []struct {
		in   string
		exe  string
		args []string
	}{
		{`"C:\Program Files\App\unins000.exe" /SILENT`, `C:\Program Files\App\unins000.exe`, []string{"/SILENT"}},
		{`C:\Tools\uninstall.exe --all`, `C:\Tools\uninstall.exe`, []string{"--all"}},
		{`MsiExec.exe /I{26A24AE4-039D-4CA4-87B4-2F64210444F6}`, `MsiExec.exe`, []string{"/I{26A24AE4-039D-4CA4-87B4-2F64210444F6}"}},
		{`rundll32.exe shell32.dll,Control_RunDLL "appwiz.cpl"`, `rundll32.exe`, []string{"shell32.dll,Control_RunDLL", "appwiz.cpl"}},
		{`"C:\unins.exe"`, `C:\unins.exe`, nil},
		{`  `, ``, nil},
	} {
		exe, args := SplitCmdline(tc.in)
		if exe != tc.exe || !slices.Equal(args, tc.args) {
			t.Errorf("SplitCmdline(%q) = %q %v, want %q %v", tc.in, exe, args, tc.exe, tc.args)
		}
	}
}

func TestEntryDate(t *testing.T) {
	e := Entry{InstallDate: "20260923"}
	if e.Date() != "2026-09-23" {
		t.Fatalf("got %q", e.Date())
	}
	if (&Entry{}).Date() != "" {
		t.Fatal("missing date stays empty")
	}
}

// splitExeTail keeps the argument tail byte-identical — quoted args
// with spaces must reach the uninstaller as stored, not re-quoted.
func TestSplitExeTail(t *testing.T) {
	for _, tc := range []struct{ in, exe, tail string }{
		{`"C:\a b\u.exe" /x`, `C:\a b\u.exe`, `/x`},
		{`C:\a\u.exe /s /x="C:\p q"`, `C:\a\u.exe`, `/s /x="C:\p q"`},
		{`MsiExec.exe /I{GUID}`, `MsiExec.exe`, `/I{GUID}`},
		{``, ``, ``},
	} {
		exe, tail := splitExeTail(tc.in)
		if exe != tc.exe || tail != tc.tail {
			t.Errorf("splitExeTail(%q) = %q %q, want %q %q", tc.in, exe, tail, tc.exe, tc.tail)
		}
	}
}

// A name variant shared with another installed app must not match —
// "Python 3.11.5" stripped to "python" while "Python 3.12.1" is still
// installed names shared ground.
func TestTokensDropSharedVariants(t *testing.T) {
	other := Entry{Name: "Python 3.12.1", Key: `K2`}
	e := Entry{Name: "Python 3.11.5", Key: `K1`}
	names := e.tokens([]Entry{e, other})
	if names["python"] {
		t.Fatal("shared variant 'python' survived")
	}
	if !names["python3.11.5"] && !names["python3.115"] {
		// the exact full name must always survive
		for n := range names {
			if n == norm(e.Name) {
				return
			}
		}
		t.Fatal("exact name token was dropped")
	}
}
