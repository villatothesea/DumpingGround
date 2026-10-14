package disk

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"unicode"

	"github.com/dumpingground/dumpingground/internal/elevate"
)

// Junk category indices in JunkReport.Cats, fixed order. Safe categories
// first, then moderate, then review — matching the preset tiers.
const (
	JunkCatUserTemp   = iota // %TEMP% files older than a week — SAFE
	JunkCatWinTemp           // C:\Windows\Temp files older than a week — SAFE
	JunkCatBrowser           // per-profile browser cache dirs — SAFE
	JunkCatThumbs            // Explorer thumbnail/icon caches — SAFE
	JunkCatShader            // DirectX and vendor shader caches — SAFE
	JunkCatWER               // Windows Error Reporting archives — SAFE
	JunkCatRecycleBin        // emptied through the shell — SAFE
	JunkCatAppCaches         // Electron app cache dirs — SAFE
	JunkCatUWP               // Store app web caches — SAFE
	JunkCatDrvExtract        // driver installer extraction leftovers — SAFE
	JunkCatUpdateDl          // installed updates' download staging — MODERATE
	JunkCatDumps             // crash dumps — MODERATE
	JunkCatWinLogs           // whitelisted Windows log files — MODERATE
	JunkCatDevCaches         // go-build, npm, pip, gradle — MODERATE
	JunkCatWindowsOld        // previous Windows installation — REVIEW
	JunkCatFound             // chkdsk file fragments — REVIEW
	JunkCatWinSxS            // DISM component cleanup — REVIEW
	JunkCatOrphanData        // stale AppData dirs no installed app claims — REVIEW
	JunkCatInstallers        // downloaded setup files — REVIEW
	JunkCatCount
)

// How a JunkDir row is removed.
const (
	JunkWhole = iota // delete the entry itself — a file or a whole dir
	JunkKids         // delete matching children, keep the dir itself
	JunkShell        // handled by a shell call — the recycle bin
	JunkDism         // handled by DISM component cleanup — Path is a label
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
	// id pins the path's ancestor chain to volume:file-index
	// identities, bound when the row is measured and verified before
	// deletion — a junction swapped in meanwhile rewrites the chain
	// and JunkClean refuses to run on it.
	id *PathID
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
	// protected is the whitelist snapshot the scan ran with —
	// normProtected output, consulted by junkAdd before a row exists.
	protected []string
	// cur/curCat/lastRep drive the live "now measuring" callback —
	// reportPath throttles itself so per-file reporting stays cheap.
	cur     func(cat int, path string, files int, bytes uint64)
	curCat  int
	lastRep time.Time
}

// JunkOpts feeds FindJunk. Running reports whether an exe name (lower
// case) is currently running — used to guard live caches. The second
// result is false when the process table could not be read, and that
// unknown answer denies cleanup exactly like a running app does: an
// unreadable process table is never read as idle. Progress reports
// the category being measured, if set.
type JunkOpts struct {
	Running  func(name string) (running, known bool)
	Progress func(cat int)
	// Current, when set, receives the path being measured right now
	// plus the running file/byte totals of that directory — throttled
	// to ~25 updates a second. Done reports one category's finished
	// totals (rows, files, bytes) before the next category starts.
	Current func(cat int, path string, files int, bytes uint64)
	Done    func(cat int, dirs, files int, bytes uint64)
	// Protected is the user's path whitelist — absolute paths cleanup
	// never lists or touches. Checked at discovery and again at
	// deletion; case-insensitive, a path covers everything under it.
	Protected []string
	// AppNames are normalized installed-app names plus their install
	// folder basenames, used by the orphan-data category to tell a
	// claimed AppData folder from an abandoned one.
	AppNames []string
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
	{"Store app caches", "Store apps re-download content — sign-ins and settings stay", RustRiskSafe},
	{"Driver leftovers", "Extraction folders installers forgot — the drivers themselves are already in DriverStore", RustRiskSafe},
	{"Update downloads", "Staging files from installed updates — Windows re-downloads if needed", RustRiskModerate},
	{"Crash dumps", "Crash evidence is gone — keep it if you are diagnosing a crash right now", RustRiskModerate},
	{"Windows logs", "Setup and servicing history lost — only matters for troubleshooting", RustRiskModerate},
	{"Dev caches", "The next build re-downloads or recompiles — slower once", RustRiskModerate},
	{"Windows upgrades", "Rollback data from past installs — deleting is permanent", RustRiskReview},
	{"Chkdsk fragments", "File fragments chkdsk recovered — may hold user data, review before deleting", RustRiskReview},
	{"Windows Update cleanup", "Superseded components removed through DISM — runs for minutes and cannot be undone", RustRiskReview},
	{"Orphaned app data", "Folders no installed app claims, untouched for 60+ days — can hold settings or saves, review each one", RustRiskReview},
	{"Setup files", "Downloaded installers you can usually re-download — tick only the ones you recognize", RustRiskReview},
}

// JunkCatName reports a category's display name, for progress text.
func JunkCatName(cat int) string { return junkCatDefs[cat].name }

// FindJunk measures every junk category. It is a fixed catalog — the
// same approach cleanmgr's handlers and BleachBit's CleanerML take —
// so nothing is ever matched heuristically.
func FindJunk(opts JunkOpts) *JunkReport {
	rep := &JunkReport{protected: normProtected(opts.Protected), cur: opts.Current}
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
		rep.curCat = cat
		if opts.Progress != nil {
			opts.Progress(cat)
		}
		fn()
		if opts.Done != nil {
			dirs, files := len(rep.Cats[cat].Dirs), 0
			for _, d := range rep.Cats[cat].Dirs {
				files += d.Files
			}
			opts.Done(cat, dirs, files, rep.Cats[cat].Bytes)
		}
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
		steamSkip := guardState("steam.exe", "Steam", opts.Running)
		for _, p := range steamShaderCaches() {
			rep.junkKidsGuard(JunkCatShader, p, 0, "Steam re-downloads shaders as games run", steamSkip, "steam.exe")
		}
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
	run(JunkCatUWP, func() {
		// UWP/Store apps keep their WinINet cache beside their package
		// state; cookies and sign-ins live in sibling dirs, untouched.
		for _, pat := range []string{
			`Packages\*\AC\INetCache`,
			`Packages\*\LocalCache\Microsoft\Windows\INetCache`,
		} {
			for _, p := range globNoErr(absJoin(local, pat)) {
				rep.junkKids(JunkCatUWP, p, 0)
			}
		}
	})
	run(JunkCatDrvExtract, func() {
		// Graphics and chipset installers extract to the drive root and
		// never clean up after themselves — the installed driver lives
		// in DriverStore, so nothing references these afterwards.
		for _, p := range []string{`C:\NVIDIA`, `C:\AMD`, `C:\INTEL`} {
			if d, ok := junkWholeDir(p); ok {
				d.Warn = "Driver setup extraction folder — re-created if the installer runs again"
				rep.junkAdd(JunkCatDrvExtract, d)
			}
		}
	})
	run(JunkCatUpdateDl, func() {
		rep.junkKidsW(JunkCatUpdateDl, absJoin(windir, `SoftwareDistribution\Download`), 0,
			"Re-downloaded if a pending update install resumes — bandwidth, not breakage")
		rep.junkKidsW(JunkCatUpdateDl, absJoin(windir, `SoftwareDistribution\DeliveryOptimization`), 0,
			"Delivery Optimization state — rebuilt as updates flow")
		// The actual multi-GB peer cache lives in the NetworkService
		// profile, not SoftwareDistribution — up to 20% of the disk by
		// default. Locked in-flight pieces skip like everything else.
		rep.junkKidsW(JunkCatUpdateDl, absJoin(windir, `ServiceProfiles\NetworkService\AppData\Local\Microsoft\Windows\DeliveryOptimization\Cache`), 0,
			"Delivery Optimization's peer cache — rebuilt as updates flow")
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
			rep.junkKidsGuard(JunkCatDumps, absJoin(local, b.base, `Crashpad\reports`), 0,
				"Crash reports queued by "+b.name+" — evidence of past crashes", "", b.exe)
		}
		// Electron apps carry the same Crashpad reporter.
		for _, a := range junkAppCatalog {
			rep.junkKidsGuard(JunkCatDumps, absJoin(roaming, a.base, `Crashpad\reports`), 0,
				"Crash reports queued by "+a.name+" — evidence of past crashes", "", a.exe)
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
			`Logs\WindowsUpdate\*.etl`, `Logs\waasmedic\*.etl`,
			`Logs\MoSetup\*.log`, `Logs\NetSetup\*.log`,
			// Only archived event logs — the live .evtx channels stay.
			`System32\winevt\Logs\Archive-*.evtx`,
			// Only rolled-over update DB segments — edb.log, edb.chk,
			// tmp.edb and the .jrs reserves are live wuauserv files.
			`SoftwareDistribution\DataStore\Logs\edb0*.log`,
		} {
			rep.junkGlob(JunkCatWinLogs, absJoin(windir, pat))
		}
		// DiagTrack's rotated telemetry traces — the service re-creates
		// them as it runs.
		for _, sub := range []string{`Microsoft\Diagnosis\ETLLogs`, `Microsoft\Diagnosis\EventStore`} {
			rep.junkKids(JunkCatWinLogs, absJoin(`C:\ProgramData`, sub), 0)
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
			{absJoin(local, `NuGet\plugins-cache`), "the next dotnet restore re-downloads plugins"},
			{absJoin(local, `Yarn\Cache`), "the next yarn install re-downloads every package"},
			{absJoin(local, `pnpm\store`), "the next pnpm install re-downloads every package — files hard-linked into node_modules stay, so the freed space can be less than shown"},
			{absJoin(local, `uv\cache`), "the next uv run re-downloads packages"},
			{absJoin(local, `Mozilla\sccache`), "the next build recompiles — sccache loses its cache"},
			{absJoin(local, `deno`), "the next deno run re-fetches modules"},
			{absJoin(home, `.bun\install\cache`), "the next bun install re-downloads packages"},
			{absJoin(home, `.gradle\wrapper\dists`), "the next Gradle build re-downloads its wrapper distribution"},
		} {
			rep.junkKidsW(JunkCatDevCaches, dc.p, 0, "Re-downloads on demand — "+dc.warn)
		}
		// The verified Go module download store — the extracted module
		// trees beside it are left alone: local replace directives can
		// point into those.
		for _, gp := range filepath.SplitList(os.Getenv("GOPATH")) {
			if filepath.IsAbs(gp) {
				rep.junkKidsW(JunkCatDevCaches, filepath.Join(gp, `pkg\mod\cache`), 0,
					"Go re-downloads module archives on demand")
			}
		}
		if os.Getenv("GOPATH") == "" {
			rep.junkKidsW(JunkCatDevCaches, absJoin(home, `go\pkg\mod\cache`), 0,
				"Go re-downloads module archives on demand")
		}
		// JetBrains per-version caches and indexes — rebuilt on next start.
		for _, jb := range globNoErr(absJoin(local, `JetBrains\*`)) {
			for _, sub := range []string{"caches", "index", "tmp"} {
				rep.junkKidsW(JunkCatDevCaches, filepath.Join(jb, sub), 0,
					"JetBrains re-indexes on next launch — slower once")
			}
		}
		// The Epic launcher's embedded Chromium cache.
		for _, wc := range globNoErr(absJoin(local, `EpicGamesLauncher\Saved\webcache*`)) {
			rep.junkKidsW(JunkCatDevCaches, wc, 0,
				"the Epic launcher re-caches store pages")
		}
	})
	run(JunkCatWindowsOld, func() {
		if d, ok := junkWholeDir(`C:\Windows.old`); ok {
			d.Warn = "Deleting is permanent — the option to roll back Windows is gone"
			rep.junkAdd(JunkCatWindowsOld, d)
		}
		// Leftovers from past or failed upgrades — same permanence as
		// Windows.old. ESD is deliberately absent (needed for a reset).
		for _, p := range []string{`C:\$Windows.~BT`, `C:\$Windows.~WS`, `C:\Windows10Upgrade`, `C:\$WinREAgent`} {
			if d, ok := junkWholeDir(p); ok {
				d.Warn = "Upgrade leftovers — deleting is permanent"
				rep.junkAdd(JunkCatWindowsOld, d)
			}
		}
		// A running servicing transaction must not lose its staging.
		rep.guardCat(JunkCatWindowsOld, "tiworker.exe")
	})
	run(JunkCatFound, func() {
		// chkdsk's recovered-fragment folders sit at drive roots.
		for _, v := range Volumes() {
			for _, p := range globNoErr(filepath.Join(v.Path, "found.*")) {
				if d, ok := junkWholeDir(p); ok {
					d.Warn = "Recovered file fragments — may hold user data, open it before deleting"
					rep.junkAdd(JunkCatFound, d)
				}
			}
		}
	})
	run(JunkCatWinSxS, func() {
		if d, ok := junkWinSxS(); ok {
			rep.junkAdd(JunkCatWinSxS, d)
		}
		// DISM fights a servicing transaction over the component store.
		rep.guardCat(JunkCatWinSxS, "tiworker.exe")
	})
	run(JunkCatOrphanData, func() { rep.junkOrphanData(roaming, local, opts.AppNames) })
	run(JunkCatInstallers, func() { rep.junkInstallers(home) })
	if dl := DownloadsDir(); dl != "" && saneUserDir(dl) {
		if bytes, files := junkDirStats(dl, rep); files > 0 {
			rep.Downloads = &JunkDir{Path: dl, Bytes: bytes, Files: files,
				Base: filepath.Base(dl), Mode: JunkKids, Recycle: true}
		}
	}
	for _, cat := range rep.Cats {
		rep.Bytes += cat.Bytes
	}
	return rep
}

// orphanMinAge is how quiet a data folder must be before the question
// "does any installed app still own this" is even asked.
const orphanMinAge = 60 * 24 * time.Hour

// orphanSkip names dirs inside the user-data roots that are never
// orphaned app data — Windows' own folders, shared caches other
// categories already handle, and containers too generic to blame.
var orphanSkip = map[string]bool{
	"microsoft": true, "windows": true, "packages": true,
	"temp": true, "cache": true, "caches": true, "logs": true,
	"crashdumps": true, "commonfiles": true, "system": true,
	"fonts": true, "themes": true, "recent": true,
}

// junkOrphanData lists user-data folders nothing installed claims.
// Ownership needs an exact or ≥4-letter prefix match against the
// installed names — prefix, never substring — plus sixty quiet days
// measured at whatever depth the folder actually changed. These rows
// are personal data: settings, saves, profiles. They are listed for
// review only; the UI decides what gets ticked.
func (rep *JunkReport) junkOrphanData(roaming, local string, appNames []string) {
	// No proven inventory, no orphans: an incomplete installed-app
	// list must never read as "nothing claims these folders".
	if len(appNames) == 0 {
		return
	}
	cutoff := time.Now().Add(-orphanMinAge)
	claimed := func(dn string) bool {
		if dn == "" || orphanSkip[dn] {
			return true
		}
		for _, n := range appNames {
			if dn == n ||
				(len(dn) >= 4 && len(n) > len(dn) && strings.HasPrefix(n, dn)) ||
				(len(n) >= 4 && len(dn) > len(n) && strings.HasPrefix(dn, n)) {
				return true
			}
		}
		return false
	}
	for _, root := range []string{roaming, local, absJoin(local, "Low")} {
		if !filepath.IsAbs(root) {
			continue
		}
		kids, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, kid := range kids {
			if !kid.IsDir() || junkReparse(kid) {
				continue
			}
			if claimed(junkNorm(kid.Name())) {
				continue
			}
			p := filepath.Join(root, kid.Name())
			last := dirLastActive(p, rep)
			if last.IsZero() || !last.Before(cutoff) {
				continue
			}
			bytes, files := junkDirStats(p, rep)
			if files == 0 {
				continue
			}
			rep.junkAdd(JunkCatOrphanData, JunkDir{
				Path: p, Bytes: bytes, Files: files,
				Base: kid.Name(), Mode: JunkWhole, Recycle: true,
				Warn: "No installed app matches this name — inactive since " + last.Format("2006-01-02"),
			})
		}
	}
}

// junkInstallers lists setup files sitting in Downloads and on the
// desktops — user content, never automatically selected, recycled
// rather than erased.
func (rep *JunkReport) junkInstallers(home string) {
	if !filepath.IsAbs(home) {
		return
	}
	roots := []string{DownloadsDir(),
		filepath.Join(home, "Desktop"), filepath.Join(home, `OneDrive\Desktop`)}
	seen := map[string]bool{}
	for _, root := range roots {
		if !saneUserDir(root) {
			continue
		}
		for _, pat := range []string{"*.msi", "*.exe", "setup*.*", "install*.*"} {
			for _, p := range globNoErr(filepath.Join(root, pat)) {
				rep.reportPath(p, 0, 0)
				if seen[strings.ToLower(p)] {
					continue
				}
				info, err := os.Lstat(p)
				if err != nil || info.IsDir() || info.Size() == 0 {
					continue
				}
				seen[strings.ToLower(p)] = true
				rep.junkAdd(JunkCatInstallers, JunkDir{
					Path: p, Bytes: uint64(info.Size()), Files: 1,
					Base: filepath.Base(p), File: true, Mode: JunkWhole, Recycle: true,
					Warn: "A downloaded installer — usually re-downloadable, but tick only what you recognize",
				})
			}
		}
	}
}

// dirLastActive is the newest modification time anywhere in a tree,
// bounded at 20k entries — a folder whose contents stopped changing
// sixty days ago is quiet even though its own timestamp may lag.
func dirLastActive(root string, rep *JunkReport) time.Time {
	var last time.Time
	visited := 0
	filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rep.reportPath(p, visited, 0)
		if visited++; visited > 20000 {
			return filepath.SkipAll
		}
		if e.IsDir() && junkReparse(e) {
			return filepath.SkipDir
		}
		info, err := e.Info()
		if err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
		return nil
	})
	return last
}

// reportPath feeds the live progress callback, throttled to ~25 calls
// a second — walking calls it per entry, so the throttle keeps callback
// cost negligible. A nil report is fine: tests pass no callback.
func (rep *JunkReport) reportPath(p string, files int, bytes uint64) {
	if rep == nil || rep.cur == nil {
		return
	}
	now := time.Now()
	if now.Sub(rep.lastRep) < 40*time.Millisecond {
		return
	}
	rep.lastRep = now
	rep.cur(rep.curCat, p, files, bytes)
}

// junkNorm reduces a name to lowercase letters and digits — the same
// normalization the uninstall scanner uses to name-match folders.
func junkNorm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// junkAdd appends a measured location to a category. Filesystem rows
// are bound to their ancestor-chain identity here so JunkClean can
// prove the path it measured is the path it deletes; a path that
// cannot be pinned is marked skipped rather than trusted. Whitelisted
// rows are never listed at all.
func (rep *JunkReport) junkAdd(cat int, d JunkDir) {
	if protectedHit(d.Path, rep.protected) {
		return
	}
	if (d.Mode == JunkWhole || d.Mode == JunkKids) && filepath.IsAbs(d.Path) {
		if id, err := BindPath(d.Path, false); err == nil {
			d.id = id
		} else if d.Skip == "" {
			d.Skip = "path could not be verified"
		}
	}
	rep.Cats[cat].Dirs = append(rep.Cats[cat].Dirs, d)
	rep.Cats[cat].Bytes += d.Bytes
}

// junkKidsGuard is junkKidsW plus a process guard and an optional
// scan-time skip reason stamped on the row it appended.
func (rep *JunkReport) junkKidsGuard(cat int, root string, age time.Duration, warn, skip, guard string) {
	n := len(rep.Cats[cat].Dirs)
	rep.junkKidsW(cat, root, age, warn)
	for i := n; i < len(rep.Cats[cat].Dirs); i++ {
		d := &rep.Cats[cat].Dirs[i]
		if d.Skip == "" {
			d.Skip = skip
		}
		d.Guard = guard
	}
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
	denied := false
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			denied = denied || elevate.IsAccessDenied(err)
			return nil
		}
		rep.reportPath(p, files, bytes)
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
	if files == 0 && denied {
		// SoftwareDistribution & co. refuse even a directory listing to
		// standard users — enumerate through the elevated worker.
		bytes, files = junkKidsElevated(root, age, cutoff)
	}
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
		rep.reportPath(p, 0, 0)
		info, err := os.Lstat(p) // a symlink's target size is not the entry's
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		rep.junkAdd(cat, JunkDir{
			Path: p, Bytes: uint64(info.Size()), Files: 1,
			Base: filepath.Base(p), File: true, Mode: JunkWhole,
		})
	}
}

// junkKidsElevated counts files in a directory the current process may
// not even read — the worker lists the tree, the same age and name
// guards apply here.
func junkKidsElevated(root string, age time.Duration, cutoff time.Time) (bytes uint64, files int) {
	kids, err := elevate.ListTree(root)
	if err != nil {
		return 0, 0
	}
	for _, k := range kids {
		if k.Dir || k.Reparse {
			continue
		}
		protected := false
		for seg := range strings.SplitSeq(filepath.Dir(k.Name), `\`) {
			if junkProtected[strings.ToLower(seg)] {
				protected = true
				break
			}
		}
		if protected {
			continue
		}
		newest := k.ModUnix
		if k.CrtUnix > newest {
			newest = k.CrtUnix
		}
		if k.AccUnix > newest {
			newest = k.AccUnix
		}
		if age > 0 && time.Unix(newest, 0).After(cutoff) {
			continue
		}
		bytes += uint64(k.Size)
		files++
	}
	return bytes, files
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
// Windows dir, the profile itself or one of its document folders.
// GetTempPath falls back to USERPROFILE when TMP/TEMP are unset, so
// "inside the profile" alone must never be enough.
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
	for _, env := range []string{"USERPROFILE", "PUBLIC", "PROGRAMDATA"} {
		if v := os.Getenv(env); v != "" && strings.EqualFold(filepath.Clean(v), c) {
			return "" // the profile itself is not a temp dir
		}
	}
	// Some path component says temp — C:\Temp, %LOCALAPPDATA%\Temp,
	// a custom D:\work\tmp\scratch.
	for q := c; ; q = filepath.Dir(q) {
		b := strings.ToLower(filepath.Base(q))
		if strings.Contains(b, "temp") || strings.Contains(b, "tmp") {
			return p
		}
		if filepath.Dir(q) == q {
			break
		}
	}
	if u := os.Getenv("USERPROFILE"); u != "" && within(c, filepath.Clean(u)) {
		for _, std := range []string{"Documents", "Desktop", "Downloads", "Pictures", "Videos", "Music"} {
			if strings.EqualFold(c, filepath.Join(u, std)) {
				return ""
			}
		}
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

// junkBrowserCatalog is the fixed list of Chromium-family browsers whose
// caches are measured, with the process that must not be running. self
// marks a layout where the base dir itself is the profile (Opera).
var junkBrowserCatalog = []struct {
	name, exe, base string
	self            bool
}{
	{"Chrome", "chrome.exe", `Google\Chrome\User Data`, false},
	{"Chrome Canary", "chrome.exe", `Google\Chrome SxS\User Data`, false},
	{"Chromium", "chromium.exe", `Chromium\User Data`, false},
	{"Edge", "msedge.exe", `Microsoft\Edge\User Data`, false},
	{"Edge Dev", "msedge.exe", `Microsoft\Edge Dev\User Data`, false},
	{"Edge Beta", "msedge.exe", `Microsoft\Edge Beta\User Data`, false},
	{"Edge Canary", "msedge.exe", `Microsoft\Edge SxS\User Data`, false},
	{"Brave", "brave.exe", `BraveSoftware\Brave-Browser\User Data`, false},
	{"Vivaldi", "vivaldi.exe", `Vivaldi\User Data`, false},
	{"Arc", "arc.exe", `Arc\User Data`, false},
	{"Opera", "opera.exe", `Opera Software\Opera Stable`, true},
}

// junkCacheSubdirs are the per-profile cache dirs BleachBit's Chromium
// rules target — pure caches, never cookies, history or login data.
var junkCacheSubdirs = []string{
	"Cache", "Code Cache", "Media Cache", "GPUCache",
	"DawnGraphiteCache", "DawnWebGPUCache", "ShaderCache", "GrShaderCache",
	`Service Worker\CacheStorage`,
}

// junkRootCaches are the cache dirs Chromium keeps at the User Data
// root, shared by every profile — easy to miss because they sit outside
// the profile folders.
var junkRootCaches = []string{"ShaderCache", "GraphiteDawnCache", "GrShaderCache"}

// guardState turns a process-guard verdict into a row's Skip text.
// Running means the app's files are locked; unknown refuses the same
// way — cleanup must prove idle, not just fail to see busy.
func guardState(exe, display string, running func(string) (bool, bool)) string {
	if running == nil {
		return ""
	}
	on, known := running(exe)
	switch {
	case on:
		return display + " is running"
	case !known:
		return display + " — running state unknown"
	}
	return ""
}

// junkBrowsers measures every profile's cache dirs for each catalogued
// browser, plus Firefox's cache2. A running browser guards its rows —
// its files are locked anyway, so cleaning then is pointless.
func (rep *JunkReport) junkBrowsers(local, roaming string, running func(string) (bool, bool)) {
	for _, b := range junkBrowserCatalog {
		base := filepath.Join(local, b.base)
		var profs []string
		if b.self {
			if fi, err := os.Stat(base); err == nil && fi.IsDir() {
				profs = []string{base}
			}
		} else {
			for _, pat := range []string{"Default", "Profile *", "Guest Profile"} {
				profs = append(profs, globNoErr(filepath.Join(base, pat))...)
			}
			skip := guardState(b.exe, b.name, running)
			for _, sub := range junkRootCaches {
				rep.junkKidsGuard(JunkCatBrowser, filepath.Join(base, sub), 0, "", skip, b.exe)
			}
		}
		skip := guardState(b.exe, b.name, running)
		for _, prof := range profs {
			for _, sub := range junkCacheSubdirs {
				p := filepath.Join(prof, sub)
				if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
					continue
				}
				bytes, files := junkDirStats(p, rep)
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
	skip := guardState("firefox.exe", "Firefox", running)
	for _, prof := range globNoErr(filepath.Join(profDir, "*")) {
		for _, sub := range []string{"cache2", "startupCache", "shader-cache"} {
			p := filepath.Join(cacheRoot, filepath.Base(prof), sub)
			bytes, files := junkDirStats(p, rep)
			if files == 0 {
				continue
			}
			rep.junkAdd(JunkCatBrowser, JunkDir{
				Path: p, Bytes: bytes, Files: files,
				Base: sub, Mode: JunkKids, Skip: skip, Guard: "firefox.exe",
			})
		}
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
	{"Cursor", "cursor.exe", `Cursor`},
	{"Windsurf", "windsurf.exe", `Windsurf`},
	{"VSCodium", "vscodium.exe", `VSCodium`},
	{"Code - OSS", "code - oss.exe", `Code - OSS`},
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
func (rep *JunkReport) junkApps(roaming string, running func(string) (bool, bool)) {
	for _, a := range junkAppCatalog {
		skip := guardState(a.exe, a.name, running)
		for _, sub := range junkAppCacheSubdirs {
			p := filepath.Join(roaming, a.base, sub)
			if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
				continue
			}
			bytes, files := junkDirStats(p, rep)
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
func junkDirStats(root string, rep *JunkReport) (bytes uint64, files int) {
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rep.reportPath(p, files, bytes)
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
func JunkClean(d JunkDir, running func(string) (bool, bool), protected []string) (freed uint64, removed, skipped int, err error) {
	if d.Skip != "" {
		return 0, 0, 1, nil
	}
	// The whitelist is rechecked at the sink, not only at scan time —
	// a path protected since the scan still refuses deletion.
	if protectedHit(d.Path, normProtected(protected)) {
		return 0, 0, 1, nil
	}
	// Re-verify the row's process guard — an app launched since the
	// scan still protects its cache, and an answer of "cannot tell"
	// refuses deletion rather than gambling on it.
	if d.Guard != "" && running != nil {
		if on, known := running(d.Guard); on || !known {
			return 0, 0, 1, nil
		}
	}
	switch d.Mode {
	case JunkShell:
		if err = EmptyRecycleBin(); err != nil {
			return 0, 0, 0, err // failure frees nothing — do not claim it
		}
		return d.Bytes, d.Files, 0, nil
	case JunkDism:
		if err = dismCleanup(); err != nil {
			return 0, 0, 0, err
		}
		return d.Bytes, 1, 0, nil
	case JunkWhole:
		if err = verifyJunkPath(d); err != nil {
			return 0, 0, 0, err
		}
		if err = SafeJunkEntry(d); err != nil {
			return 0, 0, 0, err
		}
		if d.Recycle {
			err = RecycleFileOrDirSized(d.Path, d.Bytes)
			if err != nil && elevate.IsAccessDenied(err) {
				err = elevate.DeletePath(d.Path, true)
			}
		} else {
			err = os.RemoveAll(d.Path)
			if err != nil && elevate.IsAccessDenied(err) {
				err = elevate.DeletePath(d.Path, false)
			}
		}
		if err == nil {
			return d.Bytes, 1, 0, nil
		}
		return 0, 0, 0, err
	}
	if err = verifyJunkPath(d); err != nil {
		return 0, 0, 0, err
	}
	if err = SafeJunkEntry(d); err != nil {
		return 0, 0, 0, err
	}
	freed, removed, skipped = junkCleanKids(d, protected)
	return
}

// normProtected cleans the user whitelist to absolute, lowercase
// comparison strings; junkProtected covers names, this covers paths.
func normProtected(in []string) []string {
	var out []string
	for _, p := range in {
		p = filepath.Clean(strings.TrimSpace(p))
		if filepath.IsAbs(p) {
			out = append(out, strings.ToLower(strings.TrimSuffix(p, `\`)))
		}
	}
	return out
}

// ProtectedPath reports whether the whitelist covers p — the path
// itself or anything beneath a protected entry. Shared with the
// uninstall sweep, whose items are judged by the same rule.
func ProtectedPath(p string, list []string) bool {
	return protectedHit(p, normProtected(list))
}

// protectedHit reports whether the whitelist covers p — the path
// itself or anything beneath a protected entry.
func protectedHit(p string, protected []string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	c := strings.ToLower(strings.TrimSuffix(filepath.Clean(p), `\`))
	for _, q := range protected {
		if c == q || strings.HasPrefix(c, q+`\`) {
			return true
		}
	}
	return false
}

// verifyJunkPath proves the path about to be deleted is the path the
// scan measured: the ancestor chain bound at discovery time must still
// match identity-for-identity. Rows without a binding (built by hand
// in tests) get bound now — either way, an unverifiable path is never
// deleted.
func verifyJunkPath(d JunkDir) error {
	if !filepath.IsAbs(d.Path) {
		return nil
	}
	if d.id == nil {
		id, err := BindPath(d.Path, false)
		if err != nil {
			return err
		}
		d.id = id
	}
	if !d.id.Verify() {
		return errors.New("path changed since it was scanned")
	}
	return nil
}

// junkCleanKids deletes matching files inside a kids-mode dir, then any
// directories left empty. Reparse points are never touched; a dir only
// goes when it emptied by itself and is old enough under the row's
// rule — a directory created a minute ago is never collateral.
func junkCleanKids(d JunkDir, protected []string) (freed uint64, removed, skipped int) {
	cutoff := time.Now().Add(-time.Duration(d.Age) * time.Second)
	if len(protected) > 0 {
		protected = normProtected(protected)
	}
	var dirs []string
	var bin []string                // files queued for one batched shell recycle call
	binSizes := map[string]uint64{} // sizes captured before the delete
	flushRecycle := func() {
		// One SHFileOperation for the batch; a refusal falls back to
		// per-file calls so a single bad path loses nobody.
		if err := RecyclePaths(bin); err == nil {
			for _, p := range bin {
				freed += binSizes[p]
			}
			removed += len(bin)
		} else {
			for _, p := range bin {
				err := RecycleFileOrDirSized(p, binSizes[p])
				if err != nil && elevate.IsAccessDenied(err) {
					err = elevate.DeletePath(p, true)
				}
				if err == nil {
					freed += binSizes[p]
					removed++
				} else {
					skipped++
				}
			}
		}
		bin, binSizes = bin[:0], map[string]uint64{}
	}
	removeFile := func(p string, size int64) {
		if !d.Recycle {
			err := os.Remove(p)
			if err != nil && elevate.IsAccessDenied(err) {
				err = elevate.DeletePath(p, false)
			}
			if err == nil {
				freed += uint64(size)
				removed++
			} else {
				skipped++
			}
			return
		}
		bin = append(bin, p)
		binSizes[p] = uint64(size)
		if len(bin) >= 200 {
			flushRecycle()
		}
	}
	denied := false
	walked := false
	filepath.WalkDir(d.Path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			denied = denied || elevate.IsAccessDenied(err)
			return nil
		}
		walked = true
		if p == d.Path {
			return nil
		}
		if protectedHit(p, protected) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			skipped++
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
		removeFile(p, info.Size())
		return nil
	})
	if !walked && denied {
		// The whole subtree refused enumeration — list it elevated and
		// delete through the same per-file path.
		if kids, err := elevate.ListTree(d.Path); err == nil {
			for _, k := range kids {
				p := filepath.Join(d.Path, k.Name)
				switch {
				case k.Reparse:
				case k.Dir:
					dirs = append(dirs, p)
				default:
					newest := max(k.ModUnix, k.CrtUnix, k.AccUnix)
					if d.Age > 0 && time.Unix(newest, 0).After(cutoff) {
						continue
					}
					removeFile(p, k.Size)
				}
			}
		}
	}
	if len(bin) > 0 {
		flushRecycle()
	}
	// Deepest first so parents go once their children did.
	for i := len(dirs) - 1; i >= 0; i-- {
		p := dirs[i]
		if d.Age > 0 {
			if info, err := os.Lstat(p); err == nil && junkDirNewestTime(info).After(cutoff) {
				continue
			}
		}
		err := os.Remove(p) // fails on non-empty, which is fine
		if err != nil && elevate.IsAccessDenied(err) {
			elevate.DeletePath(p, false)
		}
	}
	return
}
