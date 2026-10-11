package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/dumpingground/dumpingground/internal/uninstall"
	"github.com/egoist/mygo/ui"
)

func dump(t *testing.T, tt *ui.Tester, dest, name string) {
	t.Helper()
	if dest == "" {
		return
	}
	f, err := os.Create(filepath.Join(dest, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, tt.Image())
}

func TestScreenshotStates(t *testing.T) {
	dest := os.Getenv("DG_SHOTS")
	if dest == "" {
		t.Skip("DG_SHOTS not set")
	}
	os.MkdirAll(dest, 0755)

	// Dark process page with selection and details dock.
	m := testModel(t)
	m.processTreeMode = false
	m.processRows = []process.Info{
		{PID: 10, Name: "alpha.exe", Memory: 400, Path: `C:\Windows\alpha.exe`},
		{PID: 11, PPID: 10, Name: "beta.exe", Memory: 900, Path: `C:\Windows\beta.exe`},
		{PID: 12, Name: "gamma.exe", Memory: 1, Path: `C:\Windows\gamma.exe`},
	}
	m.rebuildProcessTree()
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetDark(true)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Click("beta.exe")
	m.showDetails = true
	tt.Frame()
	dump(t, tt, dest, "proc-dark-details.png")

	// Dark disk page with results and a drilled-in map.
	m2 := testModel(t)
	m2.page = diskPage
	m2.diskSnapshot = &disk.Snapshot{
		Root: 0,
		Nodes: []disk.Node{
			{ID: 0, Parent: -1, Name: `C:\root`, Dir: true, Bytes: 1200, Allocated: 1200, Children: []int{1, 2, 4}},
			{ID: 1, Parent: 0, Name: "alpha", Dir: true, Bytes: 700, Allocated: 700, Children: []int{3, 5}},
			{ID: 2, Parent: 0, Name: "readme.txt", Bytes: 10, Allocated: 10},
			{ID: 3, Parent: 1, Name: "data.bin", Bytes: 640, Allocated: 640},
			{ID: 4, Parent: 0, Name: "beta", Dir: true, Bytes: 490, Allocated: 490},
			{ID: 5, Parent: 1, Name: "log.txt", Bytes: 60, Allocated: 60},
		},
		Bytes: 1200, Allocated: 1200, Files: 3, Method: "Directory walk",
	}
	m2.diskRoot = 1
	m2.diskNode = 1
	tt2 := ui.NewTester(m2.view, 1180, 760)
	tt2.SetDark(true)
	tt2.Frame()
	dump(t, tt2, dest, "disk-dark.png")

	// The scan-terms modal from the skipped counter.
	m2.diskInfoOpen = true
	tt2.Frame()
	dump(t, tt2, dest, "disk-info.png")

	// Settings modal.
	tt3 := ui.NewTester(m.view, 1180, 760)
	tt3.Click("Settings")
	tt3.Frame()
	dump(t, tt3, dest, "settings.png")

	// Pill tab style with a light theme and a selected process.
	m.settingsOpen = false
	m.tabStyle = "Pill"
	m.themeMode = 0
	tt4 := ui.NewTester(m.view, 1180, 760)
	tt4.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt4.Frame()
	dump(t, tt4, dest, "proc-pill-tabs.png")

	// The Rust report: presets, filter strip, project + shared rows.
	m3 := testModel(t)
	m3.page = diskPage
	m3.rustMode = true
	m3.rustReport = testRustReport()
	tt5 := ui.NewTester(m3.view, 1180, 760)
	tt5.SetDark(true)
	tt5.Frame()
	dump(t, tt5, dest, "rust-report.png")
	// Filtered by incremental, grouped by safety level.
	m3.rustFilterCat = disk.RustCatIncr
	m3.rustGroupSafe = true
	tt5.Frame()
	dump(t, tt5, dest, "rust-filtered.png")

	// The junk report: presets, the 12-badge filter strip, rows for
	// every location — including a guarded browser cache and the
	// review-only Windows.old.
	m4 := testModel(t)
	m4.page = diskPage
	m4.junkMode = true
	m4.junkReport = testJunkReport()
	tt6 := ui.NewTester(m4.view, 1180, 760)
	tt6.SetDark(true)
	tt6.Frame()
	dump(t, tt6, dest, "junk-report.png")
	m4.junkGroupRisk = true
	m4.junkSel = 0
	tt6.Frame()
	dump(t, tt6, dest, "junk-grouped.png")
	// Nested grouping — risk outer, category inner, one group collapsed.
	m4.junkGroupCat = true
	m4.junkCollapsed["r0/c1"] = true
	tt6.Frame()
	dump(t, tt6, dest, "junk-nested.png")
	// The badge legend modal.
	m4.junkLegendOpen = true
	tt6.Frame()
	dump(t, tt6, dest, "junk-legend.png")

	// The Uninstall page: a sorted table of registry entries with the
	// armed two-click row action on one row.
	m5 := testModel(t)
	m5.page = uninstallPage
	m5.uninstLoaded = true
	m5.uninstSortCol, m5.uninstSortDesc = 3, true
	m5.uninstRows = []uninstall.Entry{
		{Name: "Docker Desktop", Version: "4.44.0", Publisher: "Docker Inc.", SizeBytes: 3500 << 20, EstimatedSize: true, InstallDate: "20260920", Location: `C:\Program Files\Docker`, Uninstall: `"C:\Program Files\Docker\uninstall.exe"`},
		{Name: "Obsidian", Version: "1.9.12", Publisher: "Obsidian", SizeBytes: 1068 << 20, EstimatedSize: true, InstallDate: "20261001", Location: `C:\Users\me\AppData\Local\Obsidian`, Uninstall: `C:\Users\me\AppData\Local\Obsidian\Uninstall Obsidian.exe`},
		{Name: "Zeron Helper", Version: "0.9.1", Publisher: "Cognition", SizeBytes: 480 << 20, EstimatedSize: true, Uninstall: `MsiExec.exe /I{GUID}`},
		{Name: "Tiny App", Publisher: "Indie Dev", SizeBytes: 0, EstimatedSize: false, InstallDate: "20260101", Uninstall: `C:\Tiny\unins.exe`},
	}
	tt7 := ui.NewTester(m5.view, 1180, 760)
	tt7.SetDark(true)
	tt7.Frame()
	dump(t, tt7, dest, "uninstall.png")

	// The leftover sweep modal: confidence-tagged items with Review
	// unchecked, registry items marked, armed remove pending.
	m5.sweepName = "Obsidian"
	m5.sweepItems = []uninstall.Item{
		{Path: `C:\Users\me\AppData\Roaming\obsidian`, Kind: uninstall.KindDir, Confidence: uninstall.Likely, Why: "user-data folder named after the app", Bytes: 90 << 20},
		{Path: `C:\Users\me\AppData\Local\obsidian-updater`, Kind: uninstall.KindDir, Confidence: uninstall.Review, Why: "folder name is similar"},
		{Path: `C:\Users\me\Desktop\Obsidian.lnk`, Kind: uninstall.KindDir, Confidence: uninstall.Likely, Why: "shortcut to the app"},
		{Path: `HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\bd400747`, Kind: uninstall.KindRegKey, Confidence: uninstall.Confirmed, Why: "the entry in the installed-apps list"},
	}
	m5.sweepSel = []bool{true, false, true, true}
	m5.sweepOpen = true
	tt7.Frame()
	dump(t, tt7, dest, "uninstall-sweep.png")
}

// testJunkReport fabricates a cleanup report resembling a real measure.
func testJunkReport() *disk.JunkReport {
	rep := &disk.JunkReport{}
	add := func(cat int, path string, bytes uint64, files int) {
		d := disk.JunkDir{Path: path, Bytes: bytes, Files: files, Base: filepath.Base(path), Mode: disk.JunkKids}
		rep.Cats[cat].Dirs = append(rep.Cats[cat].Dirs, d)
		rep.Cats[cat].Bytes += bytes
		rep.Bytes += bytes
	}
	for i, c := range []struct{ name, note string; risk int }{
		{"User temp", "Apps recreate what they still need; anything touched this week is kept", disk.RustRiskSafe},
		{"Windows temp", "Same rule as user temp, for system-level temp files", disk.RustRiskSafe},
		{"Browser caches", "First page loads are slower once — logins, history, passwords and autofill all stay", disk.RustRiskSafe},
		{"Thumbnail cache", "Folder thumbnails redraw slowly until Explorer rebuilds them", disk.RustRiskSafe},
		{"Shader cache", "Games recompile shaders once — the first launch stutters a bit", disk.RustRiskSafe},
		{"Error reports", "Old crash reports gone — only matters when diagnosing past crashes", disk.RustRiskSafe},
		{"Recycle Bin", "Deleted files become unrecoverable — the bin is the last undo", disk.RustRiskSafe},
		{"App caches", "The app re-caches on next launch — sign-ins and settings stay", disk.RustRiskSafe},
		{"Update downloads", "Staging files from installed updates — Windows re-downloads if needed", disk.RustRiskModerate},
		{"Crash dumps", "Crash evidence is gone — keep it if you are diagnosing a crash right now", disk.RustRiskModerate},
		{"Windows logs", "Setup and servicing history lost — only matters for troubleshooting", disk.RustRiskModerate},
		{"Dev caches", "The next build re-downloads or recompiles — slower once", disk.RustRiskModerate},
		{"Windows upgrades", "Rollback data from past installs — deleting is permanent", disk.RustRiskReview},
	} {
		rep.Cats[i].Name, rep.Cats[i].Note, rep.Cats[i].Risk = c.name, c.note, c.risk
	}
	add(disk.JunkCatUserTemp, `C:\Users\me\AppData\Local\Temp`, 4<<30, 4300)
	add(disk.JunkCatWinTemp, `C:\Windows\Temp`, 700<<20, 120)
	add(disk.JunkCatBrowser, `…\User Data\Default\Cache`, 900<<20, 5100)
	rep.Cats[disk.JunkCatBrowser].Dirs = append(rep.Cats[disk.JunkCatBrowser].Dirs,
		disk.JunkDir{Path: `…\User Data\Profile 1\GPUCache`, Bytes: 60 << 20, Files: 40,
			Base: "GPUCache", Mode: disk.JunkKids, Skip: "Chrome is running"})
	add(disk.JunkCatThumbs, `C:\Users\me\AppData\Local\Microsoft\Windows\Explorer\thumbcache_1280.db`, 220<<20, 1)
	add(disk.JunkCatShader, `C:\Users\me\AppData\Local\D3DSCache`, 410<<20, 900)
	add(disk.JunkCatWER, `C:\ProgramData\Microsoft\Windows\WER\ReportArchive`, 130<<20, 60)
	add(disk.JunkCatRecycleBin, "all drives", 6<<30, 2400)
	rep.Cats[disk.JunkCatAppCaches].Dirs = append(rep.Cats[disk.JunkCatAppCaches].Dirs,
		disk.JunkDir{Path: `…\Code\Cache`, Bytes: 800 << 20, Files: 3200,
			Base: "Cache", Mode: disk.JunkKids, Warn: "VS Code re-caches on next launch"})
	rep.Cats[disk.JunkCatAppCaches].Bytes += 800 << 20
	rep.Bytes += 800 << 20
	add(disk.JunkCatUpdateDl, `C:\Windows\SoftwareDistribution\Download`, 2<<30, 800)
	add(disk.JunkCatDumps, `C:\Windows\memory.dmp`, 3<<30, 1)
	add(disk.JunkCatWinLogs, `C:\Windows\Logs\CBS\CBS.log`, 80<<20, 1)
	add(disk.JunkCatDevCaches, `C:\Users\me\AppData\Local\go-build`, 9<<30, 40000)
	add(disk.JunkCatWindowsOld, `C:\Windows.old`, 24<<30, 0)
	rep.Downloads = &disk.JunkDir{Path: `C:\Users\me\Downloads`, Bytes: 12 << 30, Files: 210,
		Base: "Downloads", Mode: disk.JunkKids}
	return rep
}

// testRustReport fabricates a report resembling a real scan for shot tests.
func testRustReport() *disk.RustReport {
	rep := &disk.RustReport{Projects: 3, Scanned: 400 << 30}
	for i, c := range []struct{ name, note string; risk int }{
		{"Incremental caches", "Compiler fingerprints", disk.RustRiskSafe},
		{"Doc builds", "cargo doc output", disk.RustRiskSafe},
		{"Debug builds", "target/debug dirs", disk.RustRiskModerate},
		{"Release builds", "Optimized binaries", disk.RustRiskCareful},
		{"Stray files", "Loose files inside target/", disk.RustRiskReview},
		{"Registry cache", "~/.cargo/registry", disk.RustRiskSafe},
		{"Git checkouts", "~/.cargo/git", disk.RustRiskSafe},
		{"Old toolchains", "non-default toolchains", disk.RustRiskModerate},
		{"Orphan targets", "target dirs, no project", disk.RustRiskReview},
	} {
		rep.Cats[i].Name, rep.Cats[i].Note, rep.Cats[i].Risk = c.name, c.note, c.risk
	}
	add := func(cat int, path string, bytes uint64) {
		rep.Cats[cat].Dirs = append(rep.Cats[cat].Dirs, disk.RustDir{Path: path, Bytes: bytes, Base: filepath.Base(path)})
		rep.Cats[cat].Bytes += bytes
	}
	add(disk.RustCatIncr, `D:\ws\pith\target\debug\incremental`, 33<<30)
	add(disk.RustCatDebug, `D:\ws\pith\target\debug`, 144<<30)
	add(disk.RustCatRelease, `D:\ws\pith\target\release`, 10<<30)
	add(disk.RustCatStray, `D:\ws\pith\target\debug.log`, 5<<20)
	add(disk.RustCatRegistry, `C:\Users\me\.cargo\registry`, 3<<30)
	add(disk.RustCatGit, `C:\Users\me\.cargo\git`, 900<<20)
	add(disk.RustCatToolchains, `C:\Users\me\.rustup\toolchains\1.98.0-x86_64-pc-windows-msvc`, 1300<<20)
	add(disk.RustCatOrphans, `D:\ws\gone\target`, 2<<30)
	rep.Items = []disk.RustItem{
		{Path: `D:\ws\pith\target`, Incr: 33 << 30, Debug: 144 << 30, Rel: 10 << 30, Stray: 5 << 20, Total: 155 << 30, Idle: 0},
		{Path: `D:\ws\zeron\target`, Incr: 2 << 30, Debug: 24 << 30, Total: 34 << 30, Idle: 4},
		{Path: `D:\ws\old-thing\target`, Debug: 8 << 30, Rel: 1 << 30, Total: 9 << 30, Idle: 400},
	}
	return rep
}
