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
