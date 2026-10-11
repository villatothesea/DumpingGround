//go:build windows

package uninstall

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unicode/utf16"
	"unsafe"
)

// fakeLnk writes a minimal but structurally valid shell link: header,
// no IDList, and a LinkInfo block whose UTF-16 local base path is the
// given target — enough for parseLinkTarget tests, not for resolution.
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
	// The shell-link CLSID — IPersistFile::Load rejects links without it.
	copy(head[4:], []byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46})
	binary.LittleEndian.PutUint32(head[0x14:], 0x2) // HasLinkInfo
	out.Write(head)
	out.Write(li.Bytes())
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// saveLink writes a real .lnk through IShellLinkW::SetPath +
// IPersistFile::Save — the only way to fixture a link whose IDList the
// resolver genuinely consults.
func saveLink(t *testing.T, r *linkResolver, lnk, target string) {
	t.Helper()
	up, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	// IShellLinkW::SetPath — index 20.
	if comVtable(r.shell, 20, uintptr(unsafe.Pointer(up))) != 0 {
		t.Fatal("SetPath failed")
	}
	lp, err := syscall.UTF16PtrFromString(lnk)
	if err != nil {
		t.Fatal(err)
	}
	// IPersistFile::Save — index 6, fRemember=TRUE.
	if comVtable(r.pers, 6, uintptr(unsafe.Pointer(lp)), 1) != 0 {
		t.Fatal("Save failed")
	}
}

func TestParseLinkTarget(t *testing.T) {
	target := `D:\Apps\Some App\app.exe`
	data, err := os.ReadFile(fakeLnk(t, t.TempDir(), "app.lnk", target))
	if err != nil {
		t.Fatal(err)
	}
	if got := parseLinkTarget(data); got != target {
		t.Fatalf("parseLinkTarget = %q, want %q", got, target)
	}
}

func TestDeadShortcutItems(t *testing.T) {
	r := newLinkResolver()
	defer r.close()
	root := t.TempDir()
	liveExe := filepath.Join(root, "real.exe")
	goneExe := filepath.Join(root, "gone.exe")
	for _, p := range []string{liveExe, goneExe} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saveLink(t, r, filepath.Join(root, "live.lnk"), liveExe)
	dead := filepath.Join(root, "dead.lnk")
	saveLink(t, r, dead, goneExe)
	if err := os.Remove(goneExe); err != nil {
		t.Fatal(err)
	}

	items := deadShortcutItems([]string{root})
	if len(items) != 1 || items[0].Path != dead {
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
	// A group holding only an empty group: the ancestor is the
	// leftover, not the leaf — flag it once, at the top.
	os.MkdirAll(filepath.Join(root, "iZotope", "Ozone 12"), 0o755)

	items := emptyGroupItems([]string{root})
	got := map[string]bool{}
	for _, it := range items {
		got[filepath.Base(it.Path)] = true
	}
	if len(items) != 3 || !got["Qianwen"] || !got["OnlyIni"] || !got["iZotope"] {
		t.Fatalf("want Qianwen+OnlyIni+iZotope flagged, got %+v", items)
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
