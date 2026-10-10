package app

import (
	"context"
	"fmt"
	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCancelOwnsWorkerUntilCompletion(t *testing.T) {
	m := testModel(t)
	m.page = diskPage
	ctx, cancel := context.WithCancel(m.ctx)
	m.diskCancel = cancel
	m.diskStarted = time.Now()
	m.diskPath = "pending"
	m.diskProgressCh = make(chan disk.Progress, 1)
	m.diskDone = make(chan scanResult, 1)
	m.diskProgress = disk.Progress{Current: "pending", Files: 123, Bytes: 456}
	tt := ui.NewTester(m.view, 880, 580)
	if !tt.HasText("123") {
		t.Fatal("live progress is not visible")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled || m.diskCancel == nil || !m.diskCancelling {
		t.Fatal("cancellation released the worker prematurely")
	}
	m.diskProgressCh <- disk.Progress{Files: 999, Current: "stale"}
	m.diskDone <- scanResult{snapshot: &disk.Snapshot{Nodes: []disk.Node{{ID: 0, Parent: -1, Dir: true, Name: "partial"}}}, err: context.Canceled}
	tt.Frame()
	if m.diskCancel != nil || !strings.Contains(m.diskStatus, "Cancelled") {
		t.Fatalf("terminal status overwritten: %s", m.diskStatus)
	}
	tt.Frame()
	if strings.Contains(m.diskStatus, "Scanning") {
		t.Fatal("stale progress overwrote completion")
	}
}

func testModel(t *testing.T) *Model {
	t.Helper()
	m := New()
	t.Cleanup(m.close)
	m.diskVolumes = []disk.Volume{{Path: "C:\\", Label: "System", Total: 1000, Free: 400}, {Path: "D:\\", Label: "Data", Total: 2000, Free: 500}}
	m.diskDrive = "C:\\"
	m.diskPath = "C:\\"
	m.volsChecked = time.Now() // don't let a mid-test volume refresh rebuild the toolbar
	return m
}
func visible(t *testing.T, tt *ui.Tester, w, h int, name string) {
	t.Helper()
	r, ok := tt.Find(name)
	if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(w)+1 || r.Y+r.H > float32(h)+1 {
		t.Errorf("%dx%d %s bounds=%+v found=%v", w, h, name, r, ok)
	}
}
func TestWorkspaceControlsStayInsideWindow(t *testing.T) {
	for _, size := range [][2]int{{880, 580}, {1280, 800}} {
		for _, scale := range []float32{1, 1.5, 2} {
			t.Run(fmt.Sprintf("%dx%d@%.1f", size[0], size[1], scale), func(t *testing.T) {
				m := testModel(t)
				m.processRows = []process.Info{{PID: 10, Name: "fixture.exe", Memory: 1024}}
				m.rebuildProcessTree()
				tt := ui.NewTester(m.view, size[0], size[1])
				tt.SetScale(scale)
				for _, name := range []string{"Tree", "List", "fixture.exe", "Search processes", "Settings"} {
					visible(t, tt, size[0], size[1], name)
				}
				// The selection actions stay in the toolbar — disabled —
				// even before a row is selected.
				for _, name := range []string{"End process", "End process tree", "Details"} {
					visible(t, tt, size[0], size[1], name)
				}
				// Selection actions sit in the page toolbar once a row is
				// selected; the gear must still clear the caption strip.
				m.processSelected = 0
				tt.Frame()
				for _, name := range []string{"End process", "End process tree", "Details", "Settings"} {
					visible(t, tt, size[0], size[1], name)
				}
				if err := tt.Click("Disk Usage"); err != nil {
					t.Fatal(err)
				}
				if m.page != diskPage {
					t.Fatal("page did not switch")
				}
				for _, name := range []string{"Scan", "Browse…", "Drive C:\\", "Drive D:\\"} {
					visible(t, tt, size[0], size[1], name)
				}
				// Chips only select the drive; swap the fake volume for a
				// temp dir, select it, then press Scan — the scan of the
				// temp dir is instant and touches nothing real.
				fake := t.TempDir()
				m.diskVolumes[1] = disk.Volume{Path: fake, Total: 2000, Free: 500}
				tt.Frame()
				if err := tt.Click("Drive " + fake); err != nil {
					t.Fatal(err)
				}
				if m.diskDrive != fake || m.diskPath != fake {
					t.Fatalf("drive chip did not select: %s %s", m.diskDrive, m.diskPath)
				}
				if err := tt.Click("Scan"); err != nil {
					t.Fatal(err)
				}
				if m.diskCancel == nil && m.diskSnapshot == nil {
					t.Fatal("Scan did not start a scan")
				}
				if m.diskCancel != nil {
					m.diskCancel()
				}
				if dest := os.Getenv("DG_CAPTURE"); dest != "" {
					f, err := os.Create(dest)
					if err == nil {
						png.Encode(f, tt.Image())
						f.Close()
					}
				}
			})
		}
	}
}
func TestScanButtonCompletesAndClearReleasesState(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "child", "large.bin"), make([]byte, 8192), 0644); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.page = diskPage
	m.diskPath = root
	tt := ui.NewTester(m.view, 1100, 760)
	if err := tt.Click("Scan"); err != nil {
		t.Fatal(err)
	}
	if m.diskCancel == nil && m.diskSnapshot == nil {
		t.Fatalf("scan did not start: status=%s texts=%v", m.diskStatus, tt.Texts())
	}
	if !tt.HasText("Scanning") {
		t.Log("scan progress text: ", tt.Texts())
	}
	deadline := time.Now().Add(5 * time.Second)
	for m.diskCancel != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
	if m.diskSnapshot == nil || m.diskCancel != nil {
		t.Fatalf("scan failed: %s", m.diskStatus)
	}
	if m.diskSnapshot.Files != 1 || m.diskSnapshot.Bytes != 8192 {
		t.Fatalf("bad scan: %+v", m.diskSnapshot)
	}
	if !tt.HasText("child/") {
		t.Fatal("completed directory missing from table")
	}
	if len(m.diskRects) == 0 {
		t.Fatal("treemap did not lay out files")
	}
	if err := tt.Click("Clear index"); err != nil {
		t.Fatal(err)
	}
	if m.diskSnapshot != nil || m.diskItems != nil || m.diskRects != nil {
		t.Fatal("clear retained scan data")
	}
	m.diskPath = t.TempDir()
	tt.Frame()
	if err := tt.Click("Scan"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for m.diskCancel != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
	if m.diskSnapshot == nil || m.diskSnapshot.Files != 0 || m.diskCancel != nil {
		t.Fatalf("rescan failed: %s", m.diskStatus)
	}
	if len(m.diskRects) != 0 {
		t.Fatal("empty result retained old rectangles")
	}
	m.diskPath = filepath.Join(t.TempDir(), "missing")
	tt.Frame()
	if err := tt.Click("Scan"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for m.diskCancel != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
	if !strings.Contains(m.diskStatus, "Scan failed") || m.diskSnapshot != nil {
		t.Fatalf("missing directory did not report error: %s", m.diskStatus)
	}
}
func TestProcessTreeAggregatesSubtrees(t *testing.T) {
	m := testModel(t)
	m.processTreeMode = true
	m.processRows = []process.Info{
		{PID: 10, Name: "parent.exe", CPU: 5, Memory: 100, Threads: 2},
		{PID: 11, PPID: 10, Name: "child.exe", CPU: 3, Memory: 50, Threads: 4, Path: `C:\bin\child.exe`},
		{PID: 12, PPID: 11, Name: "grand.exe", CPU: 2, Memory: 25, Threads: 1},
		{PID: 13, Name: "lone.exe", CPU: 7, Memory: 10, Threads: 3},
	}
	m.rebuildProcessTree()
	if got := m.displayProcess(10); got.CPU != 10 || got.Memory != 175 || got.Threads != 7 {
		t.Fatalf("subtree not aggregated: %+v", got)
	}
	if got := m.displayProcess(12); got.CPU != 2 || got.Memory != 25 {
		t.Fatalf("leaf row changed: %+v", got)
	}
	m.processTreeMode = false
	if got := m.displayProcess(10); got.CPU != 5 || got.Memory != 100 {
		t.Fatalf("list mode shows aggregate: %+v", got)
	}
}

func TestExplorerChildrenStandAsRoots(t *testing.T) {
	// The shell launches but does not own apps: they must not nest under
	// explorer.exe, or ending the shell's tree would take every app with it.
	m := testModel(t)
	m.processTreeMode = true
	m.processRows = []process.Info{
		{PID: 10, Name: "explorer.exe"},
		{PID: 11, PPID: 10, Name: "app.exe"},
		{PID: 12, PPID: 10, Name: "tool.exe"},
		{PID: 13, PPID: 11, Name: "helper.exe"},
	}
	m.rebuildProcessTree()
	if slices.Contains(m.processChildren[10], 11) || slices.Contains(m.processChildren[10], 12) {
		t.Fatalf("apps nested under the shell: %v", m.processChildren[10])
	}
	if !slices.Contains(m.processRoots, 11) || !slices.Contains(m.processRoots, 12) {
		t.Fatalf("shell children not promoted to roots: %v", m.processRoots)
	}
	if !slices.Contains(m.processChildren[11], 13) {
		t.Fatalf("real child lost with the flattening: %v", m.processChildren[11])
	}
}

func TestTopRanksOnlyVisibleRows(t *testing.T) {
	// A collapsed subtree's aggregates shadow one another down the chain:
	// without the visible-rows rule the hidden children would occupy every
	// top-ten slot and leave the other roots bare.
	m := testModel(t)
	m.processTreeMode = true
	m.processRows = []process.Info{{PID: 1, Name: "big.exe", Memory: 100}}
	for i := uint32(0); i < 9; i++ {
		m.processRows = append(m.processRows, process.Info{PID: 10 + i, PPID: 1, Name: "kid.exe", Memory: uint64(90 - i)})
	}
	m.processRows = append(m.processRows, process.Info{PID: 2, Name: "small.exe", Memory: 50})
	m.rebuildProcessTree()
	tt := ui.NewTester(m.view, 1180, 400)
	tt.Frame() // populate the outline's visible rows, collapsed to roots
	m.rankProcessColumns()
	if m.procTop[3][2] == 0 {
		t.Fatalf("visible root not ranked while hidden children were: %v", m.procTop[3])
	}
	for i := uint32(0); i < 9; i++ {
		if m.procTop[3][10+i] != 0 {
			t.Fatalf("collapsed child ranked: %v", m.procTop[3])
		}
	}
}

func TestProcessDir(t *testing.T) {
	cases := map[string]string{
		`C:\Windows\System32\x.exe`: `C:\Windows\System32`,
		`C:\x.exe`:                  `C:\`,
		`x.exe`:                     "",
		"":                          "",
		`\\server\share\x.exe`:      `\\server\share`,
	}
	for in, want := range cases {
		if got := processDir(in); got != want {
			t.Errorf("processDir(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestDetailsDockDefaultsToThird(t *testing.T) {
	m := testModel(t)
	m.processRows = []process.Info{{PID: 10, Name: "a.exe", Path: `C:\bin\a.exe`}}
	m.rebuildProcessTree()
	m.processSelected = 0
	m.showDetails = true
	tt := ui.NewTester(m.view, 1180, 760)
	tt.Frame()
	tt.Frame()
	// detailSplit is the top pane's height: the dock below should hold
	// roughly a third of the page, not nearly all of it.
	if m.detailSplit < 400 || m.detailSplit > 540 {
		t.Fatalf("detailSplit=%v does not leave a third for details", m.detailSplit)
	}
	if !tt.HasText("Directory") {
		t.Fatal("directory column missing")
	}
}

func TestProcessSearchAndSort(t *testing.T) {
	m := testModel(t)
	m.processTreeMode = false
	m.processRows = []process.Info{{PID: 10, Name: "alpha.exe", Memory: 400}, {PID: 11, PPID: 10, Name: "beta.exe", Memory: 900}, {PID: 12, Name: "gamma.exe", Memory: 1}}
	m.rebuildProcessTree()
	tt := ui.NewTester(m.view, 1100, 760)
	if err := tt.Click("Working set"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Working set"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.processFlat[0] != 11 {
		t.Fatalf("sort=%+v order=%v", m.processSort, m.processFlat)
	}
	if err := tt.Click("Search processes"); err != nil {
		t.Fatal(err)
	}
	tt.Type("beta")
	tt.Frame()
	if len(m.processFlat) != 1 || m.processFlat[0] != 11 {
		t.Fatalf("search did not filter: %v", m.processFlat)
	}
}

// Regression: the top bar's rigid controls must never be squeezed under the
// native caption strip (the gear used to land at the window's right edge).
func TestTopBarControlsClearCaption(t *testing.T) {
	for _, w := range []int{960, 1100, 1280} {
		m := testModel(t)
		m.processRows = []process.Info{{PID: 10, Name: "fixture.exe", Memory: 1024}}
		m.rebuildProcessTree()
		m.processSelected = 0
		tt := ui.NewTester(m.view, w, 700)
		tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
		r, ok := tt.Find("Settings")
		if !ok || r.W < 20 || r.X+r.W > float32(w)-140 {
			t.Fatalf("w=%d gear clipped by caption: rect=%v", w, r)
		}
	}
}
