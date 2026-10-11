//go:build windows

package uninstall

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

// fakeLnk writes a minimal but structurally valid shell link: header,
// no IDList, and a LinkInfo block whose UTF-16 local base path is the
// given target.
func fakeLnk(t *testing.T, dir, name, target string) string {
	t.Helper()
	u := utf16.Encode([]rune(target))
	liSize := 0x24 + len(u)*2 + 2
	var li bytes.Buffer
	hdr := make([]byte, 0x24)
	binary.LittleEndian.PutUint32(hdr[0x00:], uint32(liSize))
	binary.LittleEndian.PutUint32(hdr[0x04:], 0x24) // header size
	binary.LittleEndian.PutUint32(hdr[0x08:], 1)    // VolumeIDAndLocalBasePath
	binary.LittleEndian.PutUint32(hdr[0x1C:], 0x24) // LocalBasePathOffsetUnicode
	li.Write(hdr)
	for _, r := range u {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], r)
		li.Write(b[:])
	}
	li.Write([]byte{0, 0})
	var out bytes.Buffer
	head := make([]byte, 0x4C)
	binary.LittleEndian.PutUint32(head, 0x4C)
	binary.LittleEndian.PutUint32(head[0x14:], 0x2) // HasLinkInfo
	out.Write(head)
	out.Write(li.Bytes())
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeAumidLnk writes a link header with no LinkInfo — the shape of
// AppUserModelID-only links, which must never be flagged.
func fakeAumidLnk(t *testing.T, dir, name string) string {
	t.Helper()
	head := make([]byte, 0x4C)
	binary.LittleEndian.PutUint32(head, 0x4C)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, append(head, []byte("SomeApp!Entry")...), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDeadShortcutItems(t *testing.T) {
	root := t.TempDir()
	missing := fakeLnk(t, root, "dead.lnk", `C:\Nonexistent\GoneApp12345\app.exe`)
	// A link whose target still exists is not a leftover.
	live := filepath.Join(root, "real.exe")
	if err := os.WriteFile(live, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeLnk(t, root, "live.lnk", live)
	// A link with no LinkInfo (AppUserModelID style) is not flagged.
	fakeAumidLnk(t, root, "aumid.lnk")

	items := deadShortcutItems([]string{root})
	if len(items) != 1 || items[0].Path != missing {
		t.Fatalf("want only the dead link flagged, got %+v", items)
	}
	if items[0].Confidence != Confirmed {
		t.Fatalf("dead link must be Confirmed, got %v", items[0].Confidence)
	}
}

func TestEmptyGroupItems(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Qianwen"), 0o755) // empty → flag
	os.MkdirAll(filepath.Join(root, "OnlyIni"), 0o755) // desktop.ini only → flag
	os.WriteFile(filepath.Join(root, "OnlyIni", "desktop.ini"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, "Startup"), 0o755) // system group → never
	os.MkdirAll(filepath.Join(root, "Full"), 0o755)    // has content → never
	os.WriteFile(filepath.Join(root, "Full", "a.lnk"), []byte("x"), 0o644)

	items := emptyGroupItems([]string{root})
	got := map[string]bool{}
	for _, it := range items {
		got[filepath.Base(it.Path)] = true
	}
	if len(items) != 2 || !got["Qianwen"] || !got["OnlyIni"] {
		t.Fatalf("want Qianwen+OnlyIni flagged, got %+v", items)
	}
}

func TestMissingTarget(t *testing.T) {
	live := filepath.Join(t.TempDir(), "real.exe")
	if err := os.WriteFile(live, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dead := `C:\Nonexistent\GoneApp12345\app.exe`

	cases := []struct {
		in   string
		want string
	}{
		{`"` + dead + `" /S`, dead},                 // quoted missing path
		{dead + ` /silent`, dead},                   // unquoted missing path
		{`"` + live + `" /S`, ""},                   // target alive
		{`rundll32.exe "` + dead + `",Entry`, dead}, // the DLL is the target
		{`regedit /s x.reg`, ""},                    // no absolute path at all
		{"", ""},                                    // empty
		{`%WINDIR%\explorer.exe`, ""},               // env-expanded, alive
	}
	for _, c := range cases {
		if got := missingTarget(c.in); got != c.want {
			t.Errorf("missingTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStillInstalled(t *testing.T) {
	installed := []Entry{
		{Name: "Obsidian", Location: `D:\Programs\Obsidian`},
	}
	if !stillInstalled("Obsidian", installed) {
		t.Fatal("Obsidian is installed")
	}
	// A decorated display name still resolves to the same app.
	if !stillInstalled("Obsidian (User)", installed) {
		t.Fatal("name variant should match the installed entry")
	}
	if stillInstalled("千问办公", installed) {
		t.Fatal("uninstalled CJK app must not match")
	}
}
