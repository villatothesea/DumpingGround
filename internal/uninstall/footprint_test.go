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
	mk(root, "Obsidian")                   // exact — Likely
	mk(root, "ObsidianTools")              // prefix — Review
	pub := mk(root, "ExampleCorp")         // publisher container
	mk(pub, "Obsidian")                    // child under publisher — Likely
	other := mk(root, "OtherApp")          // claimed by another entry
	shared := mk(root, "Microsoft")        // generic — never flagged
	mk(shared, "Edge")                     // inside a generic vendor dir
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
	if !has(Review, filepath.Join(root, "ObsidianTools")) {
		t.Error("prefix-similar dir should be Review")
	}
	if !has(Likely, filepath.Join(short, "Obsidian.lnk")) {
		t.Error("app-named shortcut should be Likely")
	}
	for _, p := range paths(Review) {
		if p == filepath.Join(root, "OtherApp") || p == shared {
			t.Errorf("claimed/generic dir must never be flagged: %s", p)
		}
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
