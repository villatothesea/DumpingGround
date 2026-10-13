package uninstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFsFootprint(t *testing.T) {
	root := t.TempDir()
	short := t.TempDir()

	// App dirs: exact name match, publisher child, prefix-similar,
	// and a dir that belongs to a different installed app.
	mk := func(root, name string) string {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mk(root, "Obsidian")                // exact — Likely
	prefix := mk(root, "ObsidianTools") // prefix-similar — never listed
	pub := mk(root, "ExampleCorp")      // publisher container
	mk(pub, "Obsidian")                 // child under publisher — Likely
	other := mk(root, "OtherApp")       // claimed by another entry
	shared := mk(root, "Microsoft")     // generic — never flagged
	mk(shared, "Edge")                  // inside a generic vendor dir
	loc := mk(t.TempDir(), "Install")
	if err := os.WriteFile(filepath.Join(loc, "app.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(short, "Obsidian.lnk"), []byte("lnk"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &Entry{Name: "Obsidian", Publisher: "ExampleCorp", Location: loc}
	all := []Entry{*e, {Name: "OtherApp", Location: other}}

	items := fsFootprint(e, all, []string{root}, []string{short})
	byConf := map[Confidence][]Item{}
	for _, it := range items {
		byConf[it.Confidence] = append(byConf[it.Confidence], it)
	}
	paths := func(c Confidence) []string {
		var out []string
		for _, it := range byConf[c] {
			out = append(out, it.Path)
		}
		return out
	}
	has := func(c Confidence, want string) bool {
		for _, p := range paths(c) {
			if p == want {
				return true
			}
		}
		return false
	}
	if !has(Confirmed, loc) {
		t.Error("install location should be Confirmed")
	}
	if !has(Likely, filepath.Join(root, "Obsidian")) {
		t.Error("exact name dir should be Likely")
	}
	if !has(Likely, filepath.Join(pub, "Obsidian")) {
		t.Error("app dir under publisher dir should be Likely")
	}
	if has(Review, prefix) || has(Likely, prefix) || has(Confirmed, prefix) {
		t.Error("similar-but-inexact dir must never be flagged")
	}
	if !has(Likely, filepath.Join(short, "Obsidian.lnk")) {
		t.Error("app-named shortcut should be Likely")
	}
	for _, items := range byConf {
		for _, it := range items {
			if it.Path == filepath.Join(root, "OtherApp") || it.Path == shared {
				t.Errorf("claimed/generic dir must never be flagged: %s", it.Path)
			}
		}
	}
}

// A CJK app name used to normalize to an empty string — and an empty
// match string flags every file scanned. Chinese names must survive
// normalization and match by exact normalized equality only.
func TestNormCJK(t *testing.T) {
	n := norm("千问办公")
	if n != "千问办公" {
		t.Fatalf("CJK name must survive norm: %q", n)
	}
	root := t.TempDir()
	mk := filepath.Join(root, "别的程序")
	if err := os.MkdirAll(mk, 0o755); err != nil {
		t.Fatal(err)
	}
	e := &Entry{Name: "千问办公"}
	if items := fsFootprint(e, []Entry{*e}, []string{root}, nil); len(items) != 0 {
		t.Fatalf("unrelated dir must not be flagged: %v", items)
	}
}

func TestNorm(t *testing.T) {
	if norm("Docker Desktop (x64)") != "dockerdesktopx64" {
		t.Fatalf("norm strips separators only: %q", norm("Docker Desktop (x64)"))
	}
	if norm("Obsidian") != "obsidian" {
		t.Fatal("lowercase")
	}
}

func TestItemDataNature(t *testing.T) {
	// HKLM + Software trees are machine-wide settings — listed, not
	// preselected; the app's own Uninstall record is neither.
	own := Item{Path: `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\App`, Kind: KindRegKey}
	if own.Personal() || !own.MachineWide() {
		t.Fatal("own uninstall record must stay a plain Confirmed row")
	}
	hklm := Item{Path: `HKLM\SOFTWARE\Vendor\App`, Kind: KindRegKey}
	if !hklm.Personal() || !hklm.MachineWide() {
		t.Fatal("HKLM Software key must be personal + machine-wide")
	}
	hkcu := Item{Path: `HKCU\SOFTWARE\Vendor\App`, Kind: KindRegKey}
	if !hkcu.Personal() || hkcu.MachineWide() {
		t.Fatal("HKCU Software key must be personal, user-scoped")
	}
	run := Item{Path: `HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Run`, Kind: KindRegValue}
	if run.Personal() {
		t.Fatal("a startup pointer is not personal data")
	}
	app := os.Getenv("APPDATA")
	if app == "" {
		t.Skip("APPDATA unset")
	}
	pers := Item{Path: filepath.Join(app, "Foo", "Data"), Kind: KindDir}
	if !pers.Personal() {
		t.Fatal("non-disposable data folder must read as personal")
	}
	// A disposable leaf stays non-personal.
	cd := Item{Path: filepath.Join(app, "Cache"), Kind: KindDir}
	if cd.Personal() {
		t.Fatal("a Cache-named folder is disposable, not personal")
	}
	pf := Item{Path: `C:\Program Files\Foo`, Kind: KindDir}
	if pf.Personal() || pf.MachineWide() {
		t.Fatal("install folder is neither personal data nor ProgramData")
	}
}

func TestChannelStrip(t *testing.T) {
	for name, want := range map[string]string{
		"Zed Nightly":                "zed",
		"Firefox Developer Edition":  "firefox",
		"Zed Preview 1.2":            "zed",
		"Chrome Canary":              "chrome",
		"Visual Studio Code Insiders": "visualstudiocode",
	} {
		if !nameTokens(name)[want] {
			t.Errorf("nameTokens(%q) missing %q", name, want)
		}
	}
}
