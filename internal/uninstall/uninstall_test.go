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

func TestMergeDupes(t *testing.T) {
	// 32/64-bit twins: same folder + name merge, richer record wins,
	// loser's key preserved in Alts.
	twin64 := Entry{Name: "App", Location: `C:\Program Files\App`, Uninstall: "u64", Key: "a64", SizeBytes: 5}
	twin32 := Entry{Name: "App", Location: `C:\PROGRAM FILES\APP`, Uninstall: "u32", Key: "a32"}
	got := mergeDupes([]Entry{twin32, twin64})
	if len(got) != 1 || len(got[0].Alts) != 1 {
		t.Fatalf("twins did not merge: %+v", got)
	}
	if got[0].Key != "a64" || got[0].Alts[0].Key != "a32" {
		t.Fatalf("richer record lost: %+v", got[0])
	}

	// Identical uninstall commands merge even across names.
	a := Entry{Name: "App", Uninstall: `"C:\u.exe"`, Key: "k1"}
	b := Entry{Name: "App (x64)", Uninstall: `"C:\u.exe"`, Key: "k2"}
	if len(mergeDupes([]Entry{a, b})) != 1 {
		t.Fatal("same uninstaller did not merge")
	}

	// Same name+version+publisher merges without a shared folder.
	c := Entry{Name: "App", Version: "1.0", Publisher: "ACME", Key: "k3"}
	d := Entry{Name: "App", Version: "1.0", Publisher: "acme", Key: "k4"}
	if len(mergeDupes([]Entry{c, d})) != 1 {
		t.Fatal("name+version+publisher did not merge")
	}

	// Different install folders never merge.
	e := Entry{Name: "App", Version: "1.0", Publisher: "ACME", Location: `C:\A`, Uninstall: "ua", Key: "k5"}
	f := Entry{Name: "App", Version: "1.0", Publisher: "ACME", Location: `D:\B`, Uninstall: "ub", Key: "k6"}
	if n := len(mergeDupes([]Entry{e, f})); n != 2 {
		t.Fatalf("different folders merged (%d rows)", n)
	}
}
