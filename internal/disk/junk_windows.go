//go:build windows

package disk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// junkReparse reports whether a walked entry is any kind of reparse
// point — symlink, junction or mount point — so cleanup never follows
// or removes one. ModeSymlink alone misses junctions on Windows.
func junkReparse(e os.DirEntry) bool {
	if e.Type()&os.ModeSymlink != 0 {
		return true
	}
	info, err := e.Info()
	if err != nil {
		return true // cannot inspect — do not touch
	}
	return fileReparse(info)
}

// junkPathReparse is junkReparse for a path rather than a walk entry —
// deletion-gate checks for whole directories and rust artifacts. A
// path that cannot be inspected is not a reparse point, it is just
// failed — the caller's own stat error handles that case.
func junkPathReparse(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fileReparse(info)
}

// fileReparse reads the attribute word a stat already fetched.
func fileReparse(info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if w, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return w.FileAttributes&0x400 != 0 // FILE_ATTRIBUTE_REPARSE_POINT
	}
	return false
}

// junkNewestTime is the age rule's clock for FILES: the newest of
// modified, created and accessed — a file still being read every day
// is not stale, whatever its mtime says.
func junkNewestTime(info os.FileInfo) time.Time {
	t := info.ModTime()
	if w, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		if ct := time.Unix(0, w.CreationTime.Nanoseconds()); ct.After(t) {
			t = ct
		}
		if at := time.Unix(0, w.LastAccessTime.Nanoseconds()); at.After(t) {
			t = at
		}
	}
	return t
}

// junkDirNewestTime ages directories by mtime/ctime only — enumerating
// a dir bumps its access time, so atime would always read "now" for a
// dir the cleaner itself just walked.
func junkDirNewestTime(info os.FileInfo) time.Time {
	t := info.ModTime()
	if w, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		if ct := time.Unix(0, w.CreationTime.Nanoseconds()); ct.After(t) {
			t = ct
		}
	}
	return t
}

var (
	junkShell32          = syscall.NewLazyDLL("shell32.dll")
	junkOle32            = syscall.NewLazyDLL("ole32.dll")
	procSHQueryRecycle   = junkShell32.NewProc("SHQueryRecycleBinW")
	procSHEmptyRecycle   = junkShell32.NewProc("SHEmptyRecycleBinW")
	procSHGetKnownFolder = junkShell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree    = junkOle32.NewProc("CoTaskMemFree")
)

// folderIDDownloads is FOLDERID_Downloads — resolves the real folder
// even when the user redirected it off C:.
var folderIDDownloads = syscall.GUID{
	Data1: 0x374DE290, Data2: 0x123F, Data3: 0x4565,
	Data4: [8]byte{0x91, 0x64, 0x39, 0xC4, 0x92, 0x5E, 0x46, 0x7B},
}

// DownloadsDir returns the user's Downloads folder via the shell's
// known-folder API, falling back to the profile default.
func DownloadsDir() string {
	var p *uint16
	hr, _, _ := procSHGetKnownFolder.Call(
		uintptr(unsafe.Pointer(&folderIDDownloads)), 0, 0,
		uintptr(unsafe.Pointer(&p)))
	if int32(hr) >= 0 && p != nil {
		defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
		n := 0
		for q := p; *q != 0; q = (*uint16)(unsafe.Add(unsafe.Pointer(q), 2)) {
			n++
		}
		return syscall.UTF16ToString(unsafe.Slice(p, n))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

// shQueryRBInfo mirrors SHQUERYRBINFO.
type shQueryRBInfo struct {
	size  int32
	bytes int64
	items int64
}

const sherbNoUI = 0x1 | 0x2 | 0x4 // no confirmation, no progress, no sound

// RecycleBinStats returns the bin's total size and item count across all
// drives — the same SHQueryRecycleBin the cleanup tools use.
func RecycleBinStats() (bytes, items uint64) {
	var q shQueryRBInfo
	q.size = int32(unsafe.Sizeof(q))
	hr, _, _ := procSHQueryRecycle.Call(0, uintptr(unsafe.Pointer(&q)))
	if int32(hr) < 0 {
		return 0, 0
	}
	return uint64(q.bytes), uint64(q.items)
}

// EmptyRecycleBin empties every drive's bin through the shell — proper
// bookkeeping, unlike deleting $Recycle.Bin contents by hand.
func EmptyRecycleBin() error {
	hr, _, _ := procSHEmptyRecycle.Call(0, 0, sherbNoUI)
	if int32(hr) < 0 {
		return fmt.Errorf("SHEmptyRecycleBin: 0x%08x", uint32(hr))
	}
	return nil
}

// steamPath reads Steam's install dir from the registry — the default
// C:\Program Files (x86)\Steam is only right on a plain install, and
// Steam is one of the most relocated apps there is.
func steamPath() string {
	var h syscall.Handle
	p, err := syscall.UTF16PtrFromString(`Software\Valve\Steam`)
	if err != nil {
		return ""
	}
	if err = syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, p, 0, syscall.KEY_READ, &h); err != nil {
		return ""
	}
	defer syscall.RegCloseKey(h)
	n, err := syscall.UTF16PtrFromString("SteamPath")
	if err != nil {
		return ""
	}
	var typ, size uint32
	r, _, _ := procRegQueryValue.Call(uintptr(h), uintptr(unsafe.Pointer(n)), 0,
		uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if r != 0 || (typ != 1 && typ != 2) || size < 4 {
		return ""
	}
	buf := make([]uint16, size/2)
	r, _, _ = procRegQueryValue.Call(uintptr(h), uintptr(unsafe.Pointer(n)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

var procRegQueryValue = syscall.NewLazyDLL("advapi32.dll").NewProc("RegQueryValueExW")

// steamShaderCaches locates the shader cache under the real Steam
// install plus every secondary library — games moved to another drive
// keep their shaders beside them, in that library's steamapps.
func steamShaderCaches() []string {
	seen := map[string]bool{}
	var out []string
	add := func(root string) {
		p := filepath.Join(root, `steamapps\shadercache`)
		if !seen[strings.ToLower(p)] {
			seen[strings.ToLower(p)] = true
			out = append(out, p)
		}
	}
	if root := steamPath(); root != "" {
		add(root)
		for _, lib := range steamLibraryFolders(root) {
			add(lib)
		}
	}
	add(`C:\Program Files (x86)\Steam`) // a leftover install may exist too
	return out
}

// steamLibraryFolders parses libraryfolders.vdf's "path" entries — a
// plain text file, so a line regex is enough.
var steamLibPathRe = regexp.MustCompile(`"path"\s+"([^"]+)"`)

func steamLibraryFolders(steamRoot string) []string {
	data, err := os.ReadFile(filepath.Join(steamRoot, `steamapps\libraryfolders.vdf`))
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range steamLibPathRe.FindAllSubmatch(data, -1) {
		out = append(out, strings.ReplaceAll(string(m[1]), `\\`, `\`))
	}
	return out
}

// dismCmd hides the console window DISM would flash when spawned from a
// GUI-subsystem process.
func dismCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "Dism", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

// junkWinSxS asks DISM how much of the component store is reclaimable —
// the same answer cleanmgr's "Windows Update Cleanup" gives. The row
// only appears when DISM recommends a cleanup; its Path is a label,
// not a filesystem location.
func junkWinSxS() (JunkDir, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := dismCmd(ctx, "/Online", "/English", "/Cleanup-Image", "/AnalyzeComponentStore").CombinedOutput()
	if err != nil {
		return JunkDir{}, false
	}
	var reclaim uint64
	recommended := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Backups and Disabled Features"),
			strings.HasPrefix(line, "Cache and Temporary Data"):
			if i := strings.LastIndex(line, ":"); i >= 0 {
				reclaim += dismBytes(line[i+1:])
			}
		case strings.HasPrefix(line, "Component Store Cleanup Recommended"):
			recommended = strings.HasSuffix(strings.ToLower(line), "yes")
		}
	}
	if !recommended || reclaim == 0 {
		return JunkDir{}, false
	}
	return JunkDir{
		Path: "DISM component cleanup", Bytes: reclaim, Files: -1,
		Base: "Component store", Mode: JunkDism,
		Warn: "Removes superseded Windows components — takes minutes, cannot be undone",
	}, true
}

// dismBytes parses DISM's "1.42 GB"-style figures.
var dismSizeRe = regexp.MustCompile(`([\d.,]+)\s*(Bytes|KB|MB|GB|TB)`)

func dismBytes(s string) uint64 {
	m := dismSizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	switch m[2] {
	case "TB":
		v *= 1 << 40
	case "GB":
		v *= 1 << 30
	case "MB":
		v *= 1 << 20
	case "KB":
		v *= 1 << 10
	}
	return uint64(v)
}

// dismCleanup runs cleanmgr's "Windows Update Cleanup" the way it does:
// the component store shrinks itself — files are never removed by hand.
func dismCleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	out, err := dismCmd(ctx, "/Online", "/English", "/Cleanup-Image", "/StartComponentCleanup").CombinedOutput()
	if err != nil {
		tail := strings.TrimSpace(string(out))
		if n := len(tail); n > 300 {
			tail = tail[n-300:]
		}
		return fmt.Errorf("dism StartComponentCleanup: %v — %s", err, tail)
	}
	return nil
}
