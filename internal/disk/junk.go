package disk

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Junk category indices in JunkReport.Cats, fixed order. Safe categories
// first, then moderate, then review — matching the preset tiers.
const (
	JunkCatUserTemp = iota // %TEMP% files older than a week — SAFE
	JunkCatWinTemp         // C:\Windows\Temp files older than a week — SAFE
	JunkCatBrowser         // per-profile browser cache dirs — SAFE
	JunkCatThumbs          // Explorer thumbnail/icon caches — SAFE
	JunkCatShader          // DirectX shader cache — SAFE
	JunkCatWER             // Windows Error Reporting archives — SAFE
	JunkCatRecycleBin      // emptied through the shell — SAFE
	JunkCatUpdateDl        // installed updates' download staging — MODERATE
	JunkCatDumps           // crash dumps — MODERATE
	JunkCatWinLogs         // whitelisted Windows log files — MODERATE
	JunkCatDevCaches       // go-build, npm, pip, gradle — MODERATE
	JunkCatWindowsOld      // previous Windows installation — REVIEW
	JunkCatCount
)

// How a JunkDir row is removed.
const (
	JunkWhole = iota // delete the entry itself — a file or a whole dir
	JunkKids         // delete matching children, keep the dir itself
	JunkShell        // handled by a shell call — the recycle bin
)

// junkTempAge is cleanmgr's rule for temp files: only files not modified
// in over a week are stale enough to remove.
const junkTempAge = 7 * 24 * time.Hour

// JunkDir is one removable location in a category. Path revalidation
// uses Base/File like the Rust cleanup; Mode picks the removal method;
// Age (seconds) limits JunkKids deletes to stale files; Skip, when set,
// explains why the row cannot be cleaned right now — e.g. the browser
// holding it is still running.
type JunkDir struct {
	Path  string
	Bytes uint64
	Files int
	Base  string
	File  bool
	Mode  int
	Age   int64
	Skip  string
}

// JunkCat is one junk category: a name, what deleting costs, a risk
// level (the shared Rust risk scale) and its locations.
type JunkCat struct {
	Name, Note string
	Risk       int
	Dirs       []JunkDir
	Bytes      uint64
}

// JunkReport summarizes removable junk across the fixed catalog. It is
// built from live filesystem reads, not the drive index — junk lives in
// known locations regardless of which drive was scanned.
type JunkReport struct {
	Cats  [JunkCatCount]JunkCat
	Bytes uint64
}

// JunkOpts feeds FindJunk: Running reports whether an exe name (lower
// case) is currently running — used to guard browser caches; Progress
// reports the category being measured, if set.
type JunkOpts struct {
	Running  func(name string) bool
	Progress func(cat int)
}

// junkCatDefs describes every category: what it is, and the risk level
// shown on its rows and group headers.
var junkCatDefs = [JunkCatCount]struct {
	name, note string
	risk       int
}{
	{"User temp", "Files in %TEMP% untouched for over a week", RustRiskSafe},
	{"Windows temp", "Files in C:\\Windows\\Temp untouched for over a week", RustRiskSafe},
	{"Browser caches", "Chrome, Edge and Firefox cache dirs — pages reload slower once", RustRiskSafe},
	{"Thumbnail cache", "Explorer thumbcache/iconcache — rebuilds while browsing folders", RustRiskSafe},
	{"Shader cache", "DirectX shader cache — games recompile shaders once", RustRiskSafe},
	{"Error reports", "Windows Error Reporting archives and queues", RustRiskSafe},
	{"Recycle Bin", "Files already deleted and held in the bin", RustRiskSafe},
	{"Update downloads", "Installed updates' staging files — Windows re-downloads when needed", RustRiskModerate},
	{"Crash dumps", "Minidumps and memory dumps — needed only for diagnosing crashes", RustRiskModerate},
	{"Windows logs", "Setup, servicing and firewall logs from whitelisted locations", RustRiskModerate},
	{"Dev caches", "Go, npm, pip and Gradle caches — re-downloaded on demand", RustRiskModerate},
	{"Windows.old", "Previous Windows installation — deleting removes the rollback option", RustRiskReview},
}

// JunkCatName reports a category's display name, for progress text.
func JunkCatName(cat int) string { return junkCatDefs[cat].name }

// FindJunk measures every junk category. It is a fixed catalog — the
// same approach cleanmgr's handlers and BleachBit's CleanerML take —
// so nothing is ever matched heuristically.
func FindJunk(opts JunkOpts) *JunkReport {
	rep := &JunkReport{}
	for i, n := range junkCatDefs {
		rep.Cats[i].Name, rep.Cats[i].Note, rep.Cats[i].Risk = n.name, n.note, n.risk
	}
	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	windir := os.Getenv("SystemRoot")
	if windir == "" {
		windir = `C:\Windows`
	}
	home, _ := os.UserHomeDir()

	run := func(cat int, fn func()) {
		if opts.Progress != nil {
			opts.Progress(cat)
		}
		fn()
	}
	run(JunkCatUserTemp, func() { rep.junkKids(JunkCatUserTemp, os.TempDir(), junkTempAge) })
	run(JunkCatWinTemp, func() { rep.junkKids(JunkCatWinTemp, filepath.Join(windir, "Temp"), junkTempAge) })
	run(JunkCatBrowser, func() { rep.junkBrowsers(local, roaming, opts.Running) })
	run(JunkCatThumbs, func() {
		for _, pat := range []string{"thumbcache_*.db", "iconcache*.db"} {
			rep.junkGlob(JunkCatThumbs, filepath.Join(local, `Microsoft\Windows\Explorer`, pat))
		}
	})
	run(JunkCatShader, func() { rep.junkKids(JunkCatShader, filepath.Join(local, "D3DSCache"), 0) })
	run(JunkCatWER, func() {
		for _, root := range []string{local, `C:\ProgramData`} {
			for _, sub := range []string{"ReportArchive", "ReportQueue", "ReportStore"} {
				rep.junkKids(JunkCatWER, filepath.Join(root, `Microsoft\Windows\WER`, sub), 0)
			}
		}
	})
	run(JunkCatRecycleBin, func() {
		size, count := RecycleBinStats()
		if size > 0 {
			rep.junkAdd(JunkCatRecycleBin, JunkDir{
				Path: "all drives", Bytes: size, Files: int(count),
				Base: "", Mode: JunkShell,
			})
		}
	})
	run(JunkCatUpdateDl, func() {
		rep.junkKids(JunkCatUpdateDl, filepath.Join(windir, `SoftwareDistribution\Download`), 0)
	})
	run(JunkCatDumps, func() {
		rep.junkGlob(JunkCatDumps, filepath.Join(windir, `Minidump\*.dmp`))
		rep.junkGlob(JunkCatDumps, filepath.Join(windir, "memory.dmp"))
		rep.junkGlob(JunkCatDumps, filepath.Join(windir, `LiveKernelReports\*.dmp`))
		rep.junkGlob(JunkCatDumps, filepath.Join(local, "CrashDumps", "*"))
	})
	run(JunkCatWinLogs, func() {
		for _, pat := range []string{
			`Logs\CBS\*.log`, `Panther\*.log`, `inf\setupapi.*.log`,
			`Logs\WindowsUpdate\*.etl`,
			// Only rolled-over update DB segments — edb.log, edb.chk,
			// tmp.edb and the .jrs reserves are live wuauserv files.
			`SoftwareDistribution\DataStore\Logs\edb0*.log`,
		} {
			rep.junkGlob(JunkCatWinLogs, filepath.Join(windir, pat))
		}
	})
	run(JunkCatDevCaches, func() {
		for _, p := range []string{
			filepath.Join(local, "go-build"),
			filepath.Join(local, "npm-cache"),
			filepath.Join(roaming, "npm-cache"),
			filepath.Join(local, `pip\cache`),
			filepath.Join(home, `.gradle\caches`),
		} {
			rep.junkKids(JunkCatDevCaches, p, 0)
		}
	})
	run(JunkCatWindowsOld, func() {
		if d, ok := junkWholeDir(`C:\Windows.old`); ok {
			rep.junkAdd(JunkCatWindowsOld, d)
		}
	})
	for _, cat := range rep.Cats {
		rep.Bytes += cat.Bytes
	}
	return rep
}

// junkAdd appends a measured location to a category.
func (rep *JunkReport) junkAdd(cat int, d JunkDir) {
	rep.Cats[cat].Dirs = append(rep.Cats[cat].Dirs, d)
	rep.Cats[cat].Bytes += d.Bytes
}

// junkKids measures a directory's matching contents — files at any
// depth whose age exceeds the gate (0 means all files) — and records it
// as a kids-mode row: the dir stays, its stale contents go.
func (rep *JunkReport) junkKids(cat int, root string, age time.Duration) {
	if root == "" {
		return
	}
	root = filepath.Clean(root)
	fi, err := os.Lstat(root)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return
	}
	var bytes uint64
	var files int
	cutoff := time.Now().Add(-age)
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if junkReparse(d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// %TEMP%\Low is the low-integrity sandbox dir apps rely
			// on existing — BleachBit whitelists it too.
			if p != root && strings.EqualFold(d.Name(), "Low") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || (age > 0 && info.ModTime().After(cutoff)) {
			return nil
		}
		bytes += uint64(info.Size())
		files++
		return nil
	})
	if files == 0 {
		return
	}
	rep.junkAdd(cat, JunkDir{
		Path: root, Bytes: bytes, Files: files,
		Base: filepath.Base(root), Mode: JunkKids, Age: int64(age.Seconds()),
	})
}

// junkGlob adds every file matching a whitelist glob as its own row.
func (rep *JunkReport) junkGlob(cat int, pattern string) {
	for _, p := range globNoErr(pattern) {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		rep.junkAdd(cat, JunkDir{
			Path: p, Bytes: uint64(info.Size()), Files: 1,
			Base: filepath.Base(p), File: true, Mode: JunkWhole,
		})
	}
}

// junkWholeDir measures a whole directory as a removable entry.
func junkWholeDir(root string) (JunkDir, bool) {
	fi, err := os.Lstat(root)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return JunkDir{}, false
	}
	bytes, _ := duDir(root)
	if bytes == 0 {
		return JunkDir{}, false
	}
	return JunkDir{Path: root, Bytes: bytes, Files: -1, Base: filepath.Base(root), Mode: JunkWhole}, true
}

// junkBrowserCatalog is the fixed list of browsers whose caches are
// measured, with the process that must not be running.
var junkBrowserCatalog = []struct {
	name, exe, base string
}{
	{"Chrome", "chrome.exe", `Google\Chrome\User Data`},
	{"Edge", "msedge.exe", `Microsoft\Edge\User Data`},
}

// junkCacheSubdirs are the cache dirs BleachBit's Chromium rules target —
// pure caches, never cookies, history or login data.
var junkCacheSubdirs = []string{
	"Cache", "Code Cache", "Media Cache", "GPUCache",
	"DawnGraphiteCache", "DawnWebGPUCache", "ShaderCache", "GrShaderCache",
}

// junkBrowsers measures every profile's cache dirs for each catalogued
// browser, plus Firefox's cache2. A running browser guards its rows —
// its files are locked anyway, so cleaning then is pointless.
func (rep *JunkReport) junkBrowsers(local, roaming string, running func(string) bool) {
	for _, b := range junkBrowserCatalog {
		base := filepath.Join(local, b.base)
		var profs []string
		for _, pat := range []string{"Default", "Profile *", "Guest Profile"} {
			profs = append(profs, globNoErr(filepath.Join(base, pat))...)
		}
		skip := ""
		if running != nil && running(b.exe) {
			skip = b.name + " is running"
		}
		for _, prof := range profs {
			for _, sub := range junkCacheSubdirs {
				p := filepath.Join(prof, sub)
				if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
					continue
				}
				bytes, files := junkDirStats(p)
				if files == 0 {
					continue
				}
				rep.junkAdd(JunkCatBrowser, JunkDir{
					Path: p, Bytes: bytes, Files: files,
					Base: filepath.Base(p), Mode: JunkKids, Skip: skip,
				})
			}
		}
	}
	// Firefox stores its cache under LocalAppData, mirroring each
	// profile name from Roaming.
	profDir := filepath.Join(roaming, `Mozilla\Firefox\Profiles`)
	cacheRoot := filepath.Join(local, `Mozilla\Firefox\Profiles`)
	skip := ""
	if running != nil && running("firefox.exe") {
		skip = "Firefox is running"
	}
	for _, prof := range globNoErr(filepath.Join(profDir, "*")) {
		c2 := filepath.Join(cacheRoot, filepath.Base(prof), "cache2")
		bytes, files := junkDirStats(c2)
		if files == 0 {
			continue
		}
		rep.junkAdd(JunkCatBrowser, JunkDir{
			Path: c2, Bytes: bytes, Files: files,
			Base: "cache2", Mode: JunkKids, Skip: skip,
		})
	}
}

// junkDirStats sums file sizes and count under a dir, skipping reparse
// points so a junction can never leak measurement or deletion elsewhere.
func junkDirStats(root string) (bytes uint64, files int) {
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if junkReparse(d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				bytes += uint64(info.Size())
				files++
			}
		}
		return nil
	})
	return
}

func globNoErr(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

// SafeJunkEntry revalidates a junk path before deletion — the entry
// must still exist and still match the recorded kind, and it must not
// be a reparse point.
func SafeJunkEntry(d JunkDir) error {
	info, err := os.Lstat(d.Path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errReparse
	}
	return SafeRustEntry(d.Path, d.Base, d.File)
}

var errReparse = os.ErrInvalid

// JunkClean removes one report row: a shell action, a whole path, or
// the matching children of a directory. Returns freed bytes, removed
// file count and how many entries were skipped because they were in
// use — locked files are routine, not failures.
func JunkClean(d JunkDir) (freed uint64, removed, skipped int, err error) {
	if d.Skip != "" {
		return 0, 0, 1, nil
	}
	switch d.Mode {
	case JunkShell:
		err = EmptyRecycleBin()
		return d.Bytes, d.Files, 0, err
	case JunkWhole:
		if err = SafeJunkEntry(d); err != nil {
			return 0, 0, 0, err
		}
		err = os.RemoveAll(d.Path)
		if err == nil {
			return d.Bytes, 1, 0, nil
		}
		return 0, 0, 0, err
	}
	if err = SafeJunkEntry(d); err != nil {
		return 0, 0, 0, err
	}
	freed, removed, skipped = junkCleanKids(d)
	return
}

// junkCleanKids deletes matching files inside a kids-mode dir, then any
// directories left empty. Reparse points are never touched.
func junkCleanKids(d JunkDir) (freed uint64, removed, skipped int) {
	cutoff := time.Now().Add(-time.Duration(d.Age) * time.Second)
	var dirs []string
	filepath.WalkDir(d.Path, func(p string, e os.DirEntry, err error) error {
		if err != nil || p == d.Path {
			return nil
		}
		if junkReparse(e) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			if strings.EqualFold(e.Name(), "Low") {
				return filepath.SkipDir
			}
			dirs = append(dirs, p)
			return nil
		}
		info, err := e.Info()
		if err != nil || (d.Age > 0 && info.ModTime().After(cutoff)) {
			return nil
		}
		if err := os.Remove(p); err != nil {
			skipped++
			return nil
		}
		freed += uint64(info.Size())
		removed++
		return nil
	})
	// Deepest first so parents go once their children did.
	for i := len(dirs) - 1; i >= 0; i-- {
		os.Remove(dirs[i]) // fails on non-empty, which is fine
	}
	return
}
