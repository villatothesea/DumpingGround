package app

import (
	"testing"

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
