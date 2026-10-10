package app

import (
	"testing"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/egoist/mygo/ui"
)

func TestProcessViewBuildsInHeadlessTester(t *testing.T) {
	m := New()
	m.processRows = []process.Info{
		{PID: 10, PPID: 1, Name: "shell.exe", CPU: 2.5, Memory: 1024},
		{PID: 11, PPID: 10, Name: "worker.exe", CPU: 1.2, Memory: 2048},
	}
	m.rebuildProcessTree()
	ui.NewTester(m.view, 1280, 800).Frame()
}

func TestDiskViewBuildsInHeadlessTester(t *testing.T) {
	m := New()
	m.page = diskPage
	m.diskSnapshot = &disk.Snapshot{
		Root: 0,
		Nodes: []disk.Node{
			{ID: 0, Parent: -1, Name: "C:\\root", Dir: true, Bytes: 30, Allocated: 30, Children: []int{1, 2}},
			{ID: 1, Parent: 0, Name: "alpha", Dir: true, Bytes: 20, Allocated: 20, Children: []int{3}},
			{ID: 2, Parent: 0, Name: "readme.txt", Bytes: 10, Allocated: 10},
			{ID: 3, Parent: 1, Name: "data.bin", Bytes: 20, Allocated: 20},
		},
		Bytes: 30, Allocated: 30,
	}
	ui.NewTester(m.view, 1280, 800).Frame()
}
