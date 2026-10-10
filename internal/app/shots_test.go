package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/process"
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

	// The Rust report: presets, collapsed category picker, project rows.
	m3 := testModel(t)
	m3.page = diskPage
	m3.rustMode = true
	m3.rustReport = testRustReport()
	tt5 := ui.NewTester(m3.view, 1180, 760)
	tt5.SetDark(true)
	tt5.Frame()
	dump(t, tt5, dest, "rust-report.png")
	m3.rustAdvanced = true
	m3.rustChecked[disk.RustCatIncr] = true
	m3.rustChecked[disk.RustCatOrphans] = true
	m3.rustSel = 100 + disk.RustCatIncr
	// Let the collapsible finish opening before capturing.
	for i := 0; i < 40; i++ {
		tt5.Frame()
	}
	dump(t, tt5, dest, "rust-advanced.png")
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
