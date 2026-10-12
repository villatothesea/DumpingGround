//go:build !windows

package disk

import (
	"os"
	"path/filepath"
	"time"
)

// DownloadsDir is the profile default off Windows.
func DownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

// RecycleBinStats is a no-op off Windows — the junk report only runs there.
func RecycleBinStats() (bytes, items uint64) { return 0, 0 }

// EmptyRecycleBin is a no-op off Windows.
func EmptyRecycleBin() error { return nil }

// junkReparse only needs the portable symlink bit elsewhere.
func junkReparse(e os.DirEntry) bool { return e.Type()&os.ModeSymlink != 0 }

// junkPathReparse is the portable symlink check off Windows — no
// junction or mount-point concept exists there.
func junkPathReparse(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// junkNewestTime only has the modification time off Windows.
func junkNewestTime(info os.FileInfo) time.Time { return info.ModTime() }

// junkDirNewestTime mirrors junkNewestTime off Windows.
func junkDirNewestTime(info os.FileInfo) time.Time { return info.ModTime() }
