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
			{ID: 0, Parent: -1, Name: `C:\root`, Path: `C:\root`, Dir: true, Bytes: 1200, Allocated: 1200, Children: []int{1, 2, 4}},
			{ID: 1, Parent: 0, Name: "alpha", Path: `C:\root\alpha`, Dir: true, Bytes: 700, Allocated: 700, Children: []int{3, 5}},
			{ID: 2, Parent: 0, Name: "readme.txt", Path: `C:\root\readme.txt`, Bytes: 10, Allocated: 10},
			{ID: 3, Parent: 1, Name: "data.bin", Path: `C:\root\alpha\data.bin`, Bytes: 640, Allocated: 640},
			{ID: 4, Parent: 0, Name: "beta", Path: `C:\root\beta`, Dir: true, Bytes: 490, Allocated: 490},
			{ID: 5, Parent: 1, Name: "log.txt", Path: `C:\root\alpha\log.txt`, Bytes: 60, Allocated: 60},
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
}
