//go:build windows

package disk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDismBytes(t *testing.T) {
	for _, c := range []struct {
		in   string
		want uint64
	}{
		{"1.5 GB", 1610612736},
		{"512.0 MB", 512 << 20},
		{"8 Bytes", 8},
		{"3 KB", 3 << 10},
		{"2.5 TB", 2748779069440},
		{"1,234.0 MB", 1234 << 20},
		{"not a size", 0},
		{"", 0},
	} {
		if got := dismBytes(c.in); got != c.want {
			t.Errorf("dismBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSteamLibraryFolders(t *testing.T) {
	root := t.TempDir()
	apps := filepath.Join(root, "steamapps")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	vdf := `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
	}
}
`
	if err := os.WriteFile(filepath.Join(apps, "libraryfolders.vdf"), []byte(vdf), 0o644); err != nil {
		t.Fatal(err)
	}
	got := steamLibraryFolders(root)
	if len(got) != 2 || got[0] != `C:\Program Files (x86)\Steam` || got[1] != `D:\SteamLibrary` {
		t.Fatalf("libraries: %v", got)
	}
	if steamLibraryFolders(t.TempDir()) != nil {
		t.Fatal("missing vdf should return nil")
	}
}
