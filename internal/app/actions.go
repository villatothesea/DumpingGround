package app

import (
	"fmt"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
)

func (m *Model) exportDisk() {
	if m.diskSnapshot == nil || m.window == nil {
		return
	}
	snapshot, window := m.diskSnapshot, m.window
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: window, Title: "Export disk scan", DefaultPath: "DumpingGround.csv", Filters: []mygo.FileFilter{{Name: "CSV", Extensions: []string{"csv"}}}})
		if err == nil && path == "" {
			return
		}
		if err == nil {
			file, e := os.Create(path)
			err = e
			if err == nil {
				err = snapshot.WriteCSV(file)
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		m.post(func() {
			if err != nil {
				m.diskStatus = "Export failed: " + err.Error()
			} else {
				m.diskStatus = "Exported " + path
			}
		})
	}()
}
func (m *Model) processMenu(menu *ui.Menu, r process.Info) {
	if menu.Item("Properties").Chosen() {
		m.showDetails = true
		m.requestInspection(r.PID)
	}
	if menu.Item("Open file location").Disabled(r.Path == "").Chosen() && m.window != nil {
		mygo.Shell.ShowItemInFolder(r.Path)
	}
	if menu.Item("Copy path").Disabled(r.Path == "").Chosen() && m.window != nil {
		mygo.Clipboard.WriteText(r.Path)
	}
	if menu.Item("Copy PID").Chosen() && m.window != nil {
		mygo.Clipboard.WriteText(fmt.Sprint(r.PID))
	}
	menu.Separator()
	if menu.Item("End process…").Disabled(r.PID <= 4).Chosen() {
		m.terminateTarget = r
		m.terminateTree = false
		m.terminateOpen = true
	}
	if menu.Item("End process tree…").Disabled(r.PID <= 4).Chosen() {
		m.terminateTarget = r
		m.terminateTree = true
		m.terminateOpen = true
	}
}
func (m *Model) diskMenu(menu *ui.Menu, id int) {
	if m.diskSnapshot == nil || id < 0 || id >= len(m.diskSnapshot.Nodes) {
		return
	}
	n := m.diskSnapshot.Nodes[id]
	path := m.diskSnapshot.FullPath(id)
	if menu.Item("Explore directory").Disabled(!n.Dir).Chosen() {
		m.navigateDisk(id)
	}
	if menu.Item("Open file location").Chosen() && m.window != nil {
		mygo.Shell.ShowItemInFolder(path)
	}
	if menu.Item("Copy path").Chosen() && m.window != nil {
		mygo.Clipboard.WriteText(path)
	}
}
