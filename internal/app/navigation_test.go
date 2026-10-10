package app

import (
	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestProcessContextMenuAndCancelEndDialog(t *testing.T) {
	m := testModel(t)
	m.processRows = []process.Info{{PID: 55, Name: "fixture.exe", Path: "C:\\fixture.exe"}}
	m.rebuildProcessTree()
	tt := ui.NewTester(m.view, 1100, 760)
	if err := tt.RightClick("fixture.exe"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Properties"); err != nil {
		t.Fatal(err)
	}
	if !m.showDetails || m.inspectedPID != 55 {
		t.Fatal("properties menu did not inspect clicked process")
	}
	if err := tt.RightClick("fixture.exe"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("End process…"); err != nil {
		t.Fatal(err)
	}
	if !m.terminateOpen || m.terminateTarget.PID != 55 {
		t.Fatal("missing end confirmation")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if m.terminateOpen {
		t.Fatal("cancel did not dismiss end confirmation")
	}
}

func TestDiskDrillDownUpAndMapAtSmallSize(t *testing.T) {
	m := testModel(t)
	m.page = diskPage
	m.diskSnapshot = &disk.Snapshot{Root: 0, Files: 2, Bytes: 100, Allocated: 100, Nodes: []disk.Node{
		{ID: 0, Parent: -1, Name: "C:\\fixture", Dir: true, Bytes: 100, Allocated: 100, Children: []int{1, 2}},
		{ID: 1, Parent: 0, Name: "child", Dir: true, Bytes: 80, Allocated: 80, Children: []int{3}},
		{ID: 2, Parent: 0, Name: "small.txt", Bytes: 20, Allocated: 20},
		{ID: 3, Parent: 1, Name: "large.bin", Bytes: 80, Allocated: 80},
	}}
	tt := ui.NewTester(m.view, 880, 580)
	if err := tt.Click("child/"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	if m.diskRoot != 1 || !tt.HasText("large.bin") {
		t.Fatalf("down failed: root=%d", m.diskRoot)
	}
	if err := tt.Click("Up"); err != nil {
		t.Fatal(err)
	}
	if m.diskRoot != 0 {
		t.Fatal("up did not navigate")
	}
	tt.SetSize(1280, 800)
	tt.Frame()
	tt.SetSize(880, 580)
	tt.Frame()
	if len(m.diskRects) == 0 || m.mapHeight < 50 {
		t.Fatalf("treemap missing after resize: h=%f", m.mapHeight)
	}
	if len(m.diskExtensions) != 2 {
		t.Fatalf("extensions=%v", m.diskExtensions)
	}
	if err := tt.Click(".txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.extensionFilter != ".txt" || len(m.diskRows) != 1 || m.diskRows[0] != 2 {
		t.Fatalf("extension filter not applied: %s %v", m.extensionFilter, m.diskRows)
	}
	if err := tt.Click("Clear type filter"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	canvas, ok := tt.Find("Treemap canvas")
	if !ok {
		t.Fatal("missing map")
	}
	var x, y float32
	found := false
	for _, r := range m.diskRects {
		if r.ID == 1 {
			x = canvas.X + r.X + r.W/2
			y = canvas.Y + r.Y + r.H/2
			found = true
			break
		}
	}
	if !found {
		t.Fatal("directory has no map rectangle")
	}
	tt.Move(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if m.diskRoot != 1 || !tt.HasText("large.bin") {
		t.Fatalf("map navigation failed: root=%d", m.diskRoot)
	}
}
