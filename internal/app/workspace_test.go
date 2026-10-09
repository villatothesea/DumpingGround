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
	if !tt.HasText("123 files") {
		t.Fatal("live progress is not visible")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled || m.diskCancel == nil || !m.diskCancelling {
		t.Fatal("cancellation released the worker prematurely")
	}
	m.diskProgressCh <- disk.Progress{Files: 999, Current: "stale"}
	m.diskDone <- scanResult{snapshot: &disk.Snapshot{Nodes: []disk.Node{{ID: 0, Parent: -1, Dir: true, Name: "partial", Path: "partial"}}}, err: context.Canceled}
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
				for _, name := range []string{"Refresh", "Processes  ·  1", "fixture.exe", "End process", "Details"} {
					visible(t, tt, size[0], size[1], name)
				}
				if err := tt.Click("Disk usage"); err != nil {
					t.Fatal(err)
				}
				if m.page != diskPage {
					t.Fatal("page did not switch")
				}
				for _, name := range []string{"Scan drive", "Scan", "Cancel", "Drive", "Browse…", "Scan D:\\"} {
					visible(t, tt, size[0], size[1], name)
				}
				if err := tt.Click("Select drive"); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyDown)
				tt.Key(0, ui.KeyEnter)
				if m.diskDrive != "D:\\" || m.diskPath != "D:\\" {
					t.Fatalf("drive selection did not update scan path: %s %s", m.diskDrive, m.diskPath)
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
func TestProcessSearchAndSort(t *testing.T) {
	m := testModel(t)
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
