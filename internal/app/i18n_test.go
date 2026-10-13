package app

import (
	"os"
	"testing"

	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/egoist/mygo/ui"
)

func TestTrTables(t *testing.T) {
	defer curLang.Store(int32(langEN))
	cases := []struct {
		lang int32
		key  string
		want string
	}{
		{int32(langZhCN), "Settings", "设置"},
		{int32(langZhTW), "Settings", "設定"},
		{int32(langJA), "Settings", "設定"},
		{int32(langKO), "Settings", "설정"},
		{int32(langEN), "Settings", "Settings"},
		{int32(langZhCN), "a key with no translation", "a key with no translation"},
	}
	for _, tc := range cases {
		curLang.Store(tc.lang)
		if got := tr(tc.key); got != tc.want {
			t.Errorf("lang %d tr(%q) = %q, want %q", tc.lang, tc.key, got, tc.want)
		}
	}
}

// Every language table must share exactly the same key set — a key
// present in zh-CN but absent from ja renders English there, which is
// acceptable, but flag accidental drift so it is a choice not a slip.
func TestTrTableKeyParity(t *testing.T) {
	tables := map[string]map[string]string{
		"zh-CN": langZhCNStrings, "zh-TW": langZhTWStrings,
		"ja": langJAStrings, "ko": langKOStrings,
	}
	for name, tab := range tables {
		for k := range langZhCNStrings {
			if _, ok := tab[k]; !ok {
				t.Errorf("%s missing key %q", name, k)
			}
		}
	}
}

// TestLocalizedScreenshot renders the process page in Simplified
// Chinese when DG_SHOTS is set — eyeball check for the whole-page
// translation and CJK glyphs.
func TestLocalizedScreenshot(t *testing.T) {
	dest := os.Getenv("DG_SHOTS")
	if dest == "" {
		t.Skip("DG_SHOTS not set")
	}
	m := testModel(t)
	m.processRows = []process.Info{
		{PID: 10, Name: "alpha.exe", Memory: 400, Path: `C:\Windows\alpha.exe`},
		{PID: 11, PPID: 10, Name: "beta.exe", Memory: 900, Path: `C:\Windows\beta.exe`},
	}
	m.rebuildProcessTree()
	defer curLang.Store(int32(langEN))
	curLang.Store(int32(langZhCN))
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()
	dump(t, tt, dest, "proc-zhCN.png")
	if !tt.HasText("进程") || !tt.HasText("搜索进程") {
		t.Error("zh-CN render missing translated labels")
	}
	curLang.Store(int32(langJA))
	tt.Frame()
	dump(t, tt, dest, "proc-ja.png")
	if !tt.HasText("プロセス") {
		t.Error("ja render missing translated labels")
	}
}
