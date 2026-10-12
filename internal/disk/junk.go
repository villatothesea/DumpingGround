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
	JunkCatUserTemp   = iota // %TEMP% files older than a week — SAFE
	JunkCatWinTemp           // C:\Windows\Temp files older than a week — SAFE
	JunkCatBrowser           // per-profile browser cache dirs — SAFE
	JunkCatThumbs            // Explorer thumbnail/icon caches — SAFE
	JunkCatShader            // DirectX shader cache — SAFE
	JunkCatWER               // Windows Error Reporting archives — SAFE
	JunkCatRecycleBin        // emptied through the shell — SAFE
	JunkCatAppCaches         // Electron app cache dirs — SAFE
	JunkCatUpdateDl          // installed updates' download staging — MODERATE
	JunkCatDumps             // crash dumps — MODERATE
	JunkCatWinLogs           // whitelisted Windows log files — MODERATE
	JunkCatDevCaches         // go-build, npm, pip, gradle — MODERATE
	JunkCatWindowsOld        // previous Windows installation — REVIEW
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
// holding it is still running. Warn is a path-specific consequence note
// that overrides the category's note — set by a handful of locations
// that deserve their own wording (the catalog is closed, so this stays
// a field, not a dictionary).
type JunkDir struct {
	Path  string
	Bytes uint64
	Files int
	Base  string
	File  bool
	Mode  int
	Age   int64
	Skip  string
	Warn  string
	// Guard names an exe that must not be running at delete time —
	// re-checked in JunkClean, since the scan-time Skip may be stale.
	Guard string
	// Recycle routes per-file deletion through the Recycle Bin —
	// user content gets undo, caches do not need it.
	Recycle bool
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
	// Downloads is measured for the reminder card only — it is user
	// content, not junk, and never enters Bytes or any preset.
	Downloads *JunkDir
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
	{"User temp", "Apps recreate what they still need; anything touched this week is kept", RustRiskSafe},
	{"Windows temp", "Same rule as user temp, for system-level temp files", RustRiskSafe},
	{"Browser caches", "First page loads are slower once — logins, history, passwords and autofill all stay", RustRiskSafe},
	{"Thumbnail cache", "Folder thumbnails redraw slowly until Explorer rebuilds them", RustRiskSafe},
	{"Shader cache", "Games recompile shaders once — the first launch stutters a bit", RustRiskSafe},
	{"Error reports", "Old crash reports gone — only matters when diagnosing past crashes", RustRiskSafe},
	{"Recycle Bin", "Deleted files become unrecoverable — the bin is the last undo", RustRiskModerate},
	{"App caches", "The app re-caches on next launch — sign-ins and settings stay", RustRiskSafe},
	{"Update downloads", "Staging files from installed updates — Windows re-downloads if needed", RustRiskModerate},
	{"Crash dumps", "Crash evidence is gone — keep it if you are diagnosing a crash right now", RustRiskModerate},
	{"Windows logs", "Setup and servicing history lost — only matters for troubleshooting", RustRiskModerate},
	{"Dev caches", "The next build re-downloads or recompiles — slower once", RustRiskModerate},
	{"Windows upgrades", "Rollback data from past installs — deleting is permanent", RustRiskReview},
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
	run(JunkCatUserTemp, func() { rep.junkKids(JunkCatUserTemp, saneTempDir(os.TempDir()), junkTempAge) })
	run(JunkCatWinTemp, func() { rep.junkKids(JunkCatWinTemp, absJoin(windir, "Temp"), junkTempAge) })
	run(JunkCatBrowser, func() {
		rep.junkBrowsers(local, roaming, opts.Running)
		// WinINet's cache — used by WebView2, Office and legacy IE apps.
		// Cookies live in the separate INetCookies dir, untouched.
		rep.junkKids(JunkCatBrowser, absJoin(local, `Microsoft\Windows\INetCache`), 0)
	})
	run(JunkCatThumbs, func() {
		for _, pat := range []string{"thumbcache_*.db", "iconcache*.db"} {
			rep.junkGlob(JunkCatThumbs, absJoin(local, `Microsoft\Windows\Explorer`, pat))
		}
	})
	run(JunkCatShader, func() {
		rep.junkKids(JunkCatShader, absJoin(local, "D3DSCache"), 0)
		// GPU vendors keep their own shader caches — much larger than
		// D3DSCache on a gaming machine.
		for _, sub := range []string{
			`NVIDIA\DXCache`, `NVIDIA\GLCache`,
			`AMD\DxCache`, `AMD\GLCache`, `Intel\ShaderCache`,
		} {
			rep.junkKids(JunkCatShader, absJoin(local, sub), 0)
		}
		rep.junkKids(JunkCatShader, `C:\Program Files (x86)\Steam\steamapps\shadercache`, 0)
	})
	run(JunkCatWER, func() {
		for _, root := range []string{local, `C:\ProgramData`} {
			for _, sub := range []string{"ReportArchive", "ReportQueue", "ReportStore"} {
				rep.junkKids(JunkCatWER, absJoin(root, `Microsoft\Windows\WER`, sub), 0)
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
	run(JunkCatAppCaches, func() { rep.junkApps(roaming, opts.Running) })
	run(JunkCatUpdateDl, func() {
		rep.junkKidsW(JunkCatUpdateDl, absJoin(windir, `SoftwareDistribution\Download`), 0,
			"Re-downloaded if a pending update install resumes — bandwidth, not breakage")
		rep.junkKidsW(JunkCatUpdateDl, absJoin(windir, `SoftwareDistribution\DeliveryOptimization`), 0,
			"Delivery Optimization's shared download cache — rebuilt as updates flow")
		// TiWorker runs while Windows is servicing — cleaning the
		// staging dir mid-transaction would break the update.
		rep.guardCat(JunkCatUpdateDl, "tiworker.exe")
	})
	run(JunkCatDumps, func() {
		rep.junkGlob(JunkCatDumps, absJoin(windir, `Minidump\*.dmp`))
		rep.junkGlob(JunkCatDumps, absJoin(windir, "memory.dmp"))
		rep.junkGlob(JunkCatDumps, absJoin(windir, `LiveKernelReports\*.dmp`))
		rep.junkGlob(JunkCatDumps, absJoin(local, "CrashDumps", "*"))
		// Chromium-family crash reporters keep pending reports per browser.
		for _, b := range junkBrowserCatalog {
			rep.junkKidsW(JunkCatDumps, absJoin(local, b.base, `Crashpad\reports`), 0,
				"Crash reports queued by "+b.name+" — evidence of past crashes")
		}
		for i := range rep.Cats[JunkCatDumps].Dirs {
			if rep.Cats[JunkCatDumps].Dirs[i].Warn == "" {
				rep.Cats[JunkCatDumps].Dirs[i].Warn = junkDumpWarn(rep.Cats[JunkCatDumps].Dirs[i].Base)
			}
		}
	})
	run(JunkCatWinLogs, func() {
		for _, pat := range []string{
			`Logs\CBS\*.log`, `Logs\CBS\CbsPersist_*.cab`, `Logs\DISM\*.log`,
			`Panther\*.log`, `inf\setupapi.*.log`,
			`Logs\WindowsUpdate\*.etl`,
			// Only rolled-over update DB segments — edb.log, edb.chk,
			// tmp.edb and the .jrs reserves are live wuauserv files.
			`SoftwareDistribution\DataStore\Logs\edb0*.log`,
		} {
			rep.junkGlob(JunkCatWinLogs, absJoin(windir, pat))
		}
	})
	run(JunkCatDevCaches, func() {
		// .m2\repository is deliberately absent — locally installed
		// artifacts and private-feed jars cannot be re-downloaded.
		for _, dc := range []struct{ p, warn string }{
			{absJoin(local, "go-build"), "the next go build recompiles everything — slower once"},
			{absJoin(local, "npm-cache"), "the next npm install re-downloads every package"},
			{absJoin(roaming, "npm-cache"), "the next npm install re-downloads every package"},
			{absJoin(local, `pip\cache`), "the next pip install re-downloads wheels"},
			{absJoin(home, `.gradle\caches`), "the next Gradle build re-downloads dependencies"},
			{absJoin(home, `.nuget\packages`), "the next dotnet restore re-downloads every package"},
			{absJoin(local, `NuGet\v3-cache`), "the next dotnet restore re-resolves versions"},
			{absJoin(local, `Yarn\Cache`), "the next yarn install re-downloads every package"},
			{absJoin(local, `pnpm\store`), "the next pnpm install re-downloads every package"},
			{absJoin(local, `uv\cache`), "the next uv run re-downloads packages"},
			{absJoin(local, `Mozilla\sccache`), "the next build recompiles — sccache loses its cache"},
		} {
			rep.junkKidsW(JunkCatDevCaches, dc.p, 0, "Re-downloads on demand — "+dc.warn)
		}
	})
	run(JunkCatWindowsOld, func() {
		if d, ok := junkWholeDir(`C:\Windows.old`); ok {
			d.Warn = "Deleting is permanent — the option to roll back Windows is gone"
			rep.junkAdd(JunkCatWindowsOld, d)
		}
		// Leftovers from past or failed upgrades — same permanence as
		// Windows.old. ESD is deliberately absent (needed for a reset).
		for _, p := range []string{`C:\$Windows.~BT`, `C:\$Windows.~WS`, `C:\Windows10Upgrade`} {
			if d, ok := junkWholeDir(p); ok {
				d.Warn = "Upgrade leftovers — deleting is permanent"
				rep.junkAdd(JunkCatWindowsOld, d)
			}
		}
		// A running servicing transaction must not lose its staging.
		rep.guardCat(JunkCatWindowsOld, "tiworker.exe")
	})
	if dl := DownloadsDir(); dl != "" && saneUserDir(dl) {
		if bytes, files := junkDirStats(dl); files > 0 {
			rep.Downloads = &JunkDir{Path: dl, Bytes: bytes, Files: files,
				Base: filepath.Base(dl), Mode: JunkKids, Recycle: true}
		}
	}
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

// junkKids measures a directory's matching contents with no warning.
func (rep *JunkReport) junkKids(cat int, root string, age time.Duration) {
	rep.junkKidsW(cat, root, age, "")
}

// junkKidsW is junkKids plus a path-specific consequence note on the row.
func (rep *JunkReport) junkKidsW(cat int, root string, age time.Duration, warn string) {
	if root == "" || !filepath.IsAbs(root) {
		return
	}
	root = filepath.Clean(root)
	fi, err := os.Lstat(root)
	if err != nil || !fi.IsDir() || junkPathReparse(root) {
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
			if p != root && junkProtected[strings.ToLower(d.Name())] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || (age > 0 && junkNewestTime(info).After(cutoff)) {
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
		Base: filepath.Base(root), Mode: JunkKids, Age: int64(age.Seconds()), Warn: warn,
	})
}

// junkDumpWarn derives a per-file consequence note from a dump's name —
// the crashed app's name is embedded in user-mode dump filenames, so no
// lookup table is needed.
func junkDumpWarn(base string) string {
	if strings.EqualFold(base, "memory.dmp") {
		return "Full system memory dump — needed only for kernel-level debugging"
	}
	if i := strings.Index(base, ".exe."); i > 0 {
		return "Evidence of an " + base[:i] + " crash — keep it if you are still diagnosing"
	}
	return "Kernel crash evidence — keep it if you are diagnosing a blue screen"
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

// junkProtected are directory names cleanup never enters or removes.
// %TEMP%\Low is the low-integrity sandbox apps rely on existing;
// Content.Outlook inside INetCache holds live copies of attachments
// the user may still be editing.
var junkProtected = map[string]bool{
	"low":             true,
	"content.outlook": true,
}

// absJoin returns "" unless base is absolute — an empty or mangled
// environment variable must never produce a relative cleanup path
// that silently lands inside the working directory.
func absJoin(base string, parts ...string) string {
	if !filepath.IsAbs(base) {
		return ""
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// within reports whether p sits inside root (or equals it).
func within(p, root string) bool {
	root = strings.TrimSuffix(filepath.Clean(root), `\`)
	if strings.EqualFold(p, root) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(p), strings.ToLower(root)+`\`)
}

// saneTempDir refuses a TEMP/TMP value that points somewhere the
// "delete week-old files" rule must never reach — a drive root, the
// Windows dir. A custom temp dir keeps working only when its name
// looks like one or it lives inside the profile.
func saneTempDir(p string) string {
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	c := filepath.Clean(p)
	if filepath.Dir(c) == c {
		return "" // TEMP=D:\ would aim the rule at a whole drive
	}
	if w := os.Getenv("SystemRoot"); w != "" && within(c, filepath.Clean(w)) {
		return ""
	}
	base := strings.ToLower(filepath.Base(c))
	if strings.Contains(base, "temp") || strings.Contains(base, "tmp") {
		return p
	}
	if u := os.Getenv("USERPROFILE"); u != "" && within(c, filepath.Clean(u)) {
		return p
	}
	return ""
}

// saneUserDir refuses a shell-known-folder path that degenerated into
// something far broader than a user folder — a drive root, the
// profile itself, Windows. Downloads redirected to D:\ must never be
// offered for cleaning.
func saneUserDir(p string) bool {
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	c := filepath.Clean(p)
	if filepath.Dir(c) == c {
		return false
	}
	for _, env := range []string{"USERPROFILE", "SystemRoot", "PUBLIC", "PROGRAMDATA"} {
		if v := os.Getenv(env); v != "" && strings.EqualFold(filepath.Clean(v), c) {
			return false
		}
	}
	return true
}

// guardCat stamps a delete-time process guard on every row in a
// category — scanned while Windows is servicing, cleaned only when it
// is not.
func (rep *JunkReport) guardCat(cat int, exe string) {
	for i := range rep.Cats[cat].Dirs {
		rep.Cats[cat].Dirs[i].Guard = exe
	}
}

// junkWholeDir measures a whole directory as a removable entry.
func junkWholeDir(root string) (JunkDir, bool) {
	fi, err := os.Lstat(root)
	if err != nil || !fi.IsDir() || junkPathReparse(root) {
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
					Base: filepath.Base(p), Mode: JunkKids, Skip: skip, Guard: b.exe,
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
			Base: "cache2", Mode: JunkKids, Skip: skip, Guard: "firefox.exe",
		})
	}
}

// junkAppCatalog is the fixed list of Electron-class apps whose cache
// dirs follow Chromium's layout. Spotify is deliberately absent — its
// Storage dir also holds user-chosen offline downloads.
var junkAppCatalog = []struct {
	name, exe, base string // base is relative to APPDATA
}{
	{"VS Code", "code.exe", `Code`},
	{"VS Code Insiders", "code-insiders.exe", `Code - Insiders`},
	{"Discord", "discord.exe", `discord`},
	{"Slack", "slack.exe", `Slack`},
	{"Microsoft Teams", "teams.exe", `Microsoft\Teams`},
	{"Signal", "signal.exe", `Signal`},
	{"Notion", "notion.exe", `Notion`},
	{"Obsidian", "obsidian.exe", `Obsidian`},
	{"Postman", "postman.exe", `Postman`},
	{"GitHub Desktop", "githubdesktop.exe", `GitHub Desktop`},
}

// junkAppCacheSubdirs are the cache dirs Electron apps share — VS Code
// adds its own Cached* variants.
var junkAppCacheSubdirs = []string{
	"Cache", "Code Cache", "GPUCache", "CachedData",
	"CachedExtensionVSIXs", "CachedProfilesData",
	`Service Worker\CacheStorage`,
}

// junkApps measures each app's cache dirs. Like the browsers, a running
// app guards its rows.
func (rep *JunkReport) junkApps(roaming string, running func(string) bool) {
	for _, a := range junkAppCatalog {
		skip := ""
		if running != nil && running(a.exe) {
			skip = a.name + " is running"
		}
		for _, sub := range junkAppCacheSubdirs {
			p := filepath.Join(roaming, a.base, sub)
			if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
				continue
			}
			bytes, files := junkDirStats(p)
			if files == 0 {
				continue
			}
			rep.junkAdd(JunkCatAppCaches, JunkDir{
				Path: p, Bytes: bytes, Files: files,
				Base: filepath.Base(p), Mode: JunkKids, Skip: skip, Guard: a.exe,
				Warn: a.name + " re-caches on next launch — sign-ins and settings stay",
			})
		}
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
// be a reparse point. ModeSymlink alone misses junctions, so the check
// goes through the file attributes.
func SafeJunkEntry(d JunkDir) error {
	if _, err := os.Lstat(d.Path); err != nil {
		return err
	}
	if junkPathReparse(d.Path) {
		return errReparse
	}
	return SafeRustEntry(d.Path, d.Base, d.File)
}

var errReparse = os.ErrInvalid

// JunkClean removes one report row: a shell action, a whole path, or
// the matching children of a directory. Returns freed bytes, removed
// file count and how many entries were skipped because they were in
// use — locked files are routine, not failures. running, when given,
// re-verifies the row's process guard: an app launched since the scan
// must still protect its cache.
func JunkClean(d JunkDir, running func(string) bool) (freed uint64, removed, skipped int, err error) {
	if d.Skip != "" || (d.Guard != "" && running != nil && running(d.Guard)) {
		return 0, 0, 1, nil
	}
	switch d.Mode {
	case JunkShell:
		if err = EmptyRecycleBin(); err != nil {
			return 0, 0, 0, err // failure frees nothing — do not claim it
		}
		return d.Bytes, d.Files, 0, nil
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
// directories left empty. Reparse points are never touched; a dir only
// goes when it emptied by itself and is old enough under the row's
// rule — a directory created a minute ago is never collateral.
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
			if junkProtected[strings.ToLower(e.Name())] {
				return filepath.SkipDir
			}
			dirs = append(dirs, p)
			return nil
		}
		info, err := e.Info()
		if err != nil || (d.Age > 0 && junkNewestTime(info).After(cutoff)) {
			return nil
		}
		// Recycle rows route through the bin — a refusal means the
		// file stays, never a silent permanent delete.
		rm := os.Remove
		if d.Recycle {
			rm = func(path string) error {
				return RecycleFileOrDirSized(path, uint64(info.Size()))
			}
		}
		if rmErr := rm(p); rmErr != nil {
			skipped++
			return nil
		}
		freed += uint64(info.Size())
		removed++
		return nil
	})
	// Deepest first so parents go once their children did.
	for i := len(dirs) - 1; i >= 0; i-- {
		p := dirs[i]
		if d.Age > 0 {
			if info, err := os.Lstat(p); err == nil && junkDirNewestTime(info).After(cutoff) {
				continue
			}
		}
		os.Remove(p) // fails on non-empty, which is fine
	}
	return
}
