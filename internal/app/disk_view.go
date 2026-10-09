package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/dumpingground/dumpingground/internal/treemap"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (m *Model) startScan() {
	if m.diskCancel != nil {
		return
	}
	path := strings.TrimSpace(m.diskPath)
	if path == "" {
		m.diskStatus = "Select a drive or folder first"
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.diskCancel = cancel
	m.diskCancelling = false
	m.diskStarted = time.Now()
	m.diskProgress = disk.Progress{Current: path}
	m.diskSnapshot = nil
	m.diskItems = nil
	m.diskRects = nil
	m.diskRows = nil
	m.diskTree = ui.OutlineState[int]{}
	m.diskSelected = -1
	m.mapRoot = -1
	m.diskViewKey = ""
	m.diskQuery = ""
	m.diskNode = -1
	m.extensionSelected = -1
	m.extensionFilter = ""
	m.mapHover = ""
	m.diskExtensions = nil
	m.diskStatus = "Scanning…"
	progress := make(chan disk.Progress, 1)
	done := make(chan scanResult, 1)
	m.diskProgressCh = progress
	m.diskDone = done
	go func() {
		snapshot, err := m.diskScanner.Scan(ctx, path, progress)
		done <- scanResult{snapshot, err}
		cancel()
	}()
}
func (m *Model) drainScan() {
	if m.diskCancel == nil {
		return
	}
	select {
	case p := <-m.diskProgressCh:
		m.diskProgress = p
	default:
	}
	// Final state is applied after progress, so queued progress can never
	// overwrite completion. Old channels are detached before another scan starts.
	select {
	case r := <-m.diskDone:
		m.diskCancel()
		m.diskCancel = nil
		m.diskProgressCh = nil
		m.diskDone = nil
		m.diskCancelling = false
		m.diskSnapshot = r.snapshot
		if r.snapshot != nil {
			m.diskRoot = r.snapshot.Root
			m.diskSelected = -1
			m.mapRoot = -1
			m.diskViewKey = ""
		}
		if errors.Is(r.err, context.Canceled) {
			m.diskStatus = "Cancelled · partial results"
		} else if r.err != nil {
			m.diskStatus = "Scan failed: " + r.err.Error()
		} else {
			m.diskStatus = fmt.Sprintf("Complete · %s · %d files · %s · %.1f s · %d skipped", r.snapshot.Method, r.snapshot.Files, format.Bytes(r.snapshot.Allocated), r.snapshot.Duration.Seconds(), r.snapshot.Skipped)
		}
	default:
	}
}
func (m *Model) cancelScan() {
	if m.diskCancel != nil {
		m.diskCancelling = true
		m.diskCancel()
		m.diskStatus = "Cancelling…"
	}
}
func (m *Model) clearDisk() {
	if m.diskCancel != nil {
		return
	}
	m.diskSnapshot = nil
	m.diskRows = nil
	m.diskItems = nil
	m.diskRects = nil
	m.diskTree = ui.OutlineState[int]{}
	m.diskFiles = ui.ListState{}
	m.diskRoot = 0
	m.diskSelected = -1
	m.diskViewKey = ""
	m.mapRoot = -1
	m.diskNode = -1
	m.extensionSelected = -1
	m.extensionFilter = ""
	m.diskExtensions = nil
	m.mapHover = ""
	m.diskStatus = "Index cleared"
}
func (m *Model) volumeOptions() []string {
	options := make([]string, 0, len(m.diskVolumes))
	for _, v := range m.diskVolumes {
		options = append(options, v.Path)
	}
	return options
}
func (m *Model) browse() {
	if m.window == nil {
		return
	}
	parent, path := m.window, m.diskPath
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: "Select folder to scan", DefaultPath: path, Directory: true})
		m.post(func() {
			if err != nil {
				m.diskStatus = err.Error()
			} else if len(paths) > 0 {
				m.diskPath = paths[0]
			}
		})
	}()
}
func (m *Model) diskView(c *ui.Context) {
	t := c.Theme()
	scanning := m.diskCancel != nil
	ui.Row(c).FillWidth().Height(40).Shrink(0).Padding(4, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Drive").Shrink(0)
		if ui.Select(c, &m.diskDrive, m.volumeOptions()).Width(95).Label("Select drive").Disabled(scanning).Changed() {
			m.diskPath = m.diskDrive
		}
		if ui.PrimaryButton(c, "Scan drive").Disabled(scanning || m.diskDrive == "").Clicked() {
			m.diskPath = m.diskDrive
			m.startScan()
		}
		info := "Detecting drives…"
		for _, v := range m.diskVolumes {
			if v.Path == m.diskDrive {
				info = fmt.Sprintf("%s   %s free of %s", v.Label, format.Bytes(v.Free), format.Bytes(v.Total))
			}
		}
		ui.Text(c, info).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted)
		if ui.Button(c, "Export CSV").Disabled(scanning || m.diskSnapshot == nil).Clicked() {
			m.exportDisk()
		}
		if ui.Button(c, "Clear index").Disabled(scanning || m.diskSnapshot == nil).Clicked() {
			m.clearDisk()
		}
	})
	ui.Row(c).FillWidth().Height(40).Shrink(0).Padding(4, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c, &m.diskPath).Label("Scan path").Placeholder("Drive or folder path").Grow(1).MinWidth(120).Disabled(scanning)
		if ui.Button(c, "Browse…").Disabled(scanning).Clicked() {
			m.browse()
		}
		if ui.PrimaryButton(c, "Scan").Disabled(scanning || strings.TrimSpace(m.diskPath) == "").Clicked() {
			m.startScan()
		}
		if ui.Button(c, "Cancel").Disabled(!scanning || m.diskCancelling).Clicked() {
			m.cancelScan()
		}
	})
	if scanning {
		p := m.diskProgress
		title := "Scanning"
		if p.Method != "" {
			title += " · " + p.Method
		}
		if m.diskCancelling {
			title = "Cancelling"
		}
		ui.Column(c).FillWidth().Shrink(0).Padding(8, 12).Gap(5).Background(t.Surface).Children(func() {
			ui.Text(c, fmt.Sprintf("%s · %d files · %d folders · %s allocated · %.1f s · %d skipped", title, p.Files, p.Dirs, format.Bytes(p.Allocated), time.Since(m.diskStarted).Seconds(), p.Skipped)).Bold().SingleLine()
			ui.Text(c, p.Current).SingleLine().Tooltip(p.Current).TextColor(t.TextMuted)
			ui.Progress(c, -1).FillWidth().Height(4)
		})
	}
	if m.diskSnapshot == nil {
		ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Padding(12).Gap(8).Children(func() {
			ui.Text(c, "Drives").Bold().FontSize(14)
			for _, volume := range m.diskVolumes {
				v := volume
				ui.Row(c).FillWidth().Height(60).Shrink(0).Padding(8).Gap(10).AlignItems(ui.Center).Background(t.Surface).Children(func() {
					ui.Text(c, v.Path).FontSize(20).Bold().Width(72)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, v.Label).SingleLine()
						ui.Text(c, fmt.Sprintf("%s used   ·   %s free   ·   %s total", format.Bytes(v.Total-v.Free), format.Bytes(v.Free), format.Bytes(v.Total))).TextColor(t.TextMuted)
						if v.Total > 0 {
							ui.Progress(c, float64(v.Total-v.Free)/float64(v.Total)).FillWidth().Height(4)
						}
					})
					if ui.Button(c, "Scan "+v.Path).Disabled(scanning).Clicked() {
						m.diskDrive = v.Path
						m.diskPath = v.Path
						m.startScan()
					}
				})
			}
			if !scanning {
				ui.Text(c, m.diskStatus).TextColor(t.TextMuted)
				ui.Text(c, "Choose a drive above, or browse to a folder. Scans run in the background.").TextColor(t.TextMuted)
			}
		})
		return
	}
	s := m.diskSnapshot
	if s.Fallback != "" {
		ui.Text(c, s.Method+" · "+s.Fallback).FillWidth().SingleLine().FontSize(11).Padding(3, 10).TextColor(t.TextMuted).Tooltip(s.Fallback)
	}
	ui.Row(c).Height(34).FillWidth().Shrink(0).Padding(3, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "Up").Disabled(m.diskRoot == s.Root).Clicked() {
			m.navigateDisk(s.Nodes[m.diskRoot].Parent)
		}
		if ui.Button(c, "Root").Disabled(m.diskRoot == s.Root).Clicked() {
			m.navigateDisk(s.Root)
		}
		path := s.FullPath(m.diskRoot)
		ui.Text(c, path).SingleLine().Grow(1).MinWidth(0).Tooltip(path)
		ui.Select(c, &m.diskMetric, []string{"Allocated", "Logical"}).Width(110).Label("Size metric")
		ui.Select(c, &m.diskMode, []string{"Tree", "Files"}).Width(88).Label("Disk view")
		ui.SearchField(c, &m.diskQuery).Width(180).Placeholder("Filter files").Label("Filter files")
	})
	ui.Row(c).Height(28).FillWidth().Shrink(0).PaddingX(12).Gap(14).AlignItems(ui.Center).Children(func() {
		root := s.Nodes[m.diskRoot]
		ui.Text(c, "Logical "+format.Bytes(root.Bytes)).Bold()
		ui.Text(c, "Allocated "+format.Bytes(root.Allocated)).Bold()
		ui.Text(c, fmt.Sprintf("%d files · %d skipped · %d estimates", s.Files, s.Skipped, s.Estimated)).TextColor(t.TextMuted)
	})
	ui.Row(c).FillWidth().Height(32).Shrink(0).Padding(3, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		label := "Select a file or folder"
		valid := m.diskNode >= 0 && m.diskNode < len(s.Nodes)
		if valid {
			label = s.FullPath(m.diskNode)
		}
		ui.Text(c, label).SingleLine().Grow(1).MinWidth(0).Tooltip(label)
		if m.extensionFilter != "" && ui.Button(c, "Clear type filter").Clicked() {
			m.extensionFilter = ""
			m.extensionSelected = -1
			m.diskViewKey = ""
			m.diskSelected = -1
		}
		if ui.Button(c, "Open location").Disabled(!valid).Clicked() && m.window != nil {
			mygo.Shell.ShowItemInFolder(label)
		}
		if ui.Button(c, "Copy path").Disabled(!valid).Clicked() && m.window != nil {
			mygo.Clipboard.WriteText(label)
		}
	})
	m.prepareDiskRows()
	m.prepareMap()
	w, h := c.Size()
	if m.diskLeft == 0 {
		m.diskLeft = w * 0.70
	}
	m.diskLeft = min(m.diskLeft, w-210)
	m.diskSplit = min(m.diskSplit, max(120, (h-270)*0.60))
	ui.SplitVertical(c, &m.diskSplit, func() { ui.Split(c, &m.diskLeft, func() { m.diskTable(c) }, func() { m.extensionTable(c) }).Fill() }, func() { m.diskMap(c) }).Grow(1).MinHeight(0).FillWidth()
	ui.Text(c, "Hard-link allocation is counted once per scan. Reparse/cloud entries are skipped; unknown allocation is estimated.").FillWidth().FontSize(11).SingleLine().Padding(3, 10).TextColor(t.TextMuted)
}
func (m *Model) navigateDisk(id int) {
	if m.diskSnapshot == nil || id < 0 || id >= len(m.diskSnapshot.Nodes) || !m.diskSnapshot.Nodes[id].Dir {
		return
	}
	m.diskRoot = id
	m.diskSelected = -1
	m.diskViewKey = ""
	m.mapRoot = -1
	m.diskTree = ui.OutlineState[int]{}
	m.diskFiles = ui.ListState{}
	m.diskNode = -1
	m.extensionSelected = -1
	m.extensionFilter = ""
	m.mapHover = ""
}
func (m *Model) prepareDiskRows() {
	key := fmt.Sprintf("%d/%s/%s/%s/%s/%t/%s", m.diskRoot, m.diskMode, m.diskMetric, m.diskQuery, m.diskSort.Column, m.diskSort.Descending, m.extensionFilter)
	if key == m.diskViewKey {
		return
	}
	m.diskViewKey = key
	s := m.diskSnapshot
	m.diskRows = m.diskRows[:0]
	query := strings.ToLower(m.diskQuery)
	if m.diskMode == "Files" || query != "" || m.extensionFilter != "" {
		stack := []int{m.diskRoot}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n := s.Nodes[id]
			if n.Dir {
				stack = append(stack, n.Children...)
			} else if (query == "" || strings.Contains(strings.ToLower(n.Name), query)) && (m.extensionFilter == "" || fileExtension(n.Name) == m.extensionFilter) {
				m.diskRows = append(m.diskRows, id)
			}
		}
	} else {
		m.diskRows = append(m.diskRows, s.Nodes[m.diskRoot].Children...)
	}
	less := func(a, b int) int {
		x, y := s.Nodes[a], s.Nodes[b]
		n := 0
		switch m.diskSort.Column {
		case "name":
			n = strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		case "logical":
			n = cmp.Compare(x.Bytes, y.Bytes)
		default:
			n = cmp.Compare(x.Allocated, y.Allocated)
		}
		if n == 0 {
			n = cmp.Compare(a, b)
		}
		if m.diskSort.Descending {
			return -n
		}
		return n
	}
	if m.diskSort.Column != "" {
		slices.SortFunc(m.diskRows, less)
	}
}
func (m *Model) diskTable(c *ui.Context) {
	s := m.diskSnapshot
	cols := []ui.TableColumn{{ID: "name", Title: "Name", Width: 280, Sortable: true}, {ID: "logical", Title: "Logical size", Width: 110, Align: ui.End, Sortable: true}, {ID: "allocated", Title: "Allocated", Width: 110, Align: ui.End, Sortable: true}, {Title: "% of folder", Width: 100, Align: ui.End}, {Title: "Path / status"}}
	cell := func(id, col int) {
		n := s.Nodes[id]
		text := ""
		switch col {
		case 0:
			text = n.Name
			if n.Dir {
				text += "/"
			}
		case 1:
			text = format.Bytes(n.Bytes)
		case 2:
			text = format.Bytes(n.Allocated)
		case 3:
			den := m.diskValue(s.Nodes[m.diskRoot])
			if den > 0 {
				text = format.Percent(100 * float64(m.diskValue(n)) / float64(den))
			}
		case 4:
			text = s.FullPath(id)
			if n.Error != "" {
				text = n.Error
			}
		}
		ui.Text(c, text).SingleLine().Tooltip(s.FullPath(id)).ContextMenu(func(menu *ui.Menu) { m.diskMenu(menu, id) })
	}
	selectedID := -1
	submitted := false
	if m.diskMode == "Tree" && m.diskQuery == "" && m.extensionFilter == "" {
		m.diskTree.List.Selected = &m.diskSelected
		m.diskTree.List.Sort = &m.diskSort
		submitted = ui.OutlineTable(c, &m.diskTree, cols, m.diskRows, func(id int) []int { return s.Nodes[id].Children }, cell).Fill().Label("Disk table").Submitted()
		if m.diskSelected >= 0 && m.diskSelected < m.diskTree.Rows() {
			selectedID = m.diskTree.Item(m.diskSelected)
		}
	} else {
		m.diskFiles.Selected = &m.diskSelected
		m.diskFiles.Sort = &m.diskSort
		m.diskFiles.Key = func(i int) any { return m.diskRows[i] }
		submitted = ui.Table(c, &m.diskFiles, cols, len(m.diskRows), func(i, col int) { cell(m.diskRows[i], col) }).Fill().Label("Disk table").Submitted()
		if m.diskSelected >= 0 && m.diskSelected < len(m.diskRows) {
			selectedID = m.diskRows[m.diskSelected]
		}
	}
	if selectedID >= 0 {
		m.diskNode = selectedID
	}
	if submitted && selectedID >= 0 {
		if s.Nodes[selectedID].Dir {
			m.navigateDisk(selectedID)
		} else {
			m.mapHover = s.FullPath(selectedID)
			if m.window != nil {
				mygo.Shell.ShowItemInFolder(m.mapHover)
			}
		}
	}
}
func (m *Model) diskValue(n disk.Node) uint64 {
	if m.diskMetric == "Logical" {
		return n.Bytes
	}
	return n.Allocated
}
func (m *Model) prepareMap() {
	if m.mapRoot == m.diskRoot && m.mapMetric == m.diskMetric {
		return
	}
	m.mapRoot = m.diskRoot
	m.mapMetric = m.diskMetric
	m.mapWidth = 0
	m.mapHeight = 0
	m.diskItems = nil
	m.diskRects = nil
	m.diskExtensions = nil
	stats := map[string]extensionStat{}
	s := m.diskSnapshot
	stack := []int{m.diskRoot}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := s.Nodes[id]
		if n.Dir {
			stack = append(stack, n.Children...)
		} else {
			v := m.diskValue(n)
			ext := strings.ToLower(filepath.Ext(n.Name))
			if ext == "" {
				ext = "(no extension)"
			}
			stat := stats[ext]
			stat.Extension = ext
			stat.Files++
			stat.Bytes += v
			stats[ext] = stat

		}
	}
	for _, stat := range stats {
		m.diskExtensions = append(m.diskExtensions, stat)
	}
	slices.SortFunc(m.diskExtensions, func(a, b extensionStat) int { return cmp.Compare(b.Bytes, a.Bytes) })

}
func (m *Model) diskMap(c *ui.Context) {
	m.prepareMap()
	t := c.Theme()
	ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).FillWidth().Height(26).Shrink(0).PaddingX(10).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Space map").Bold()
			ui.Text(c, "Color = extension · click file to locate its folder").FontSize(11).TextColor(t.TextMuted)
		})
		canvas := ui.Box(c.Key("space-map")).Grow(1).FillWidth().MinHeight(0).Clip().Background(t.Surface).Label("Treemap canvas")
		if canvas.Clicked() {
			if x, y, ok := canvas.PointerPosition(); ok {
				if id, hit := treemap.Hit(m.diskRects, x, y); hit && id >= 0 {
					if id == m.diskRoot {
						m.diskMode = "Files"
						m.diskViewKey = ""
					} else if m.diskSnapshot.Nodes[id].Dir {
						m.navigateDisk(id)
					} else {
						parent := m.diskSnapshot.Nodes[id].Parent
						if parent != m.diskRoot {
							m.navigateDisk(parent)
						}
						m.diskNode = id
						m.diskMode = "Files"
						m.diskViewKey = ""
						m.prepareDiskRows()
						m.diskSelected = slices.Index(m.diskRows, id)
						if m.diskSelected >= 0 {
							m.diskFiles.ScrollIntoView(m.diskSelected)
						}
					}
				}
			}
		}
		if x, y, ok := canvas.PointerPosition(); ok {
			if id, hit := treemap.Hit(m.diskRects, x, y); hit {
				if id < 0 {
					m.mapHover = "Other files (aggregated to keep rendering bounded)"
				} else {
					n := m.diskSnapshot.Nodes[id]
					m.mapHover = fmt.Sprintf("%s · %s", m.diskSnapshot.FullPath(id), format.Bytes(m.diskValue(n)))
				}
			}
		}
		m.prepareMap()
		canvas.Tooltip(m.mapHover).Draw(func(p *ui.Painter, r ui.Rect) {
			if r.W <= 0 || r.H <= 0 {
				return
			}
			if r.W != m.mapWidth || r.H != m.mapHeight {
				m.diskRects = nil
				if m.diskValue(m.diskSnapshot.Nodes[m.diskRoot]) > 0 {
					m.diskRects = treemap.TreeLayout(m.diskRoot, r.W, r.H, 8192, func(id int) []treemap.Item {
						kids := m.diskSnapshot.Nodes[id].Children
						items := make([]treemap.Item, 0, len(kids))
						for _, kid := range kids {
							n := m.diskSnapshot.Nodes[kid]
							items = append(items, treemap.Item{ID: kid, Value: m.diskValue(n)})
						}
						return items
					})
				}
				m.mapWidth, m.mapHeight = r.W, r.H
			}
			for _, b := range m.diskRects {
				if b.W < 1 || b.H < 1 {
					continue
				}
				label := "Other files"
				color := ui.Hex("#65748a")
				if b.ID >= 0 {
					n := m.diskSnapshot.Nodes[b.ID]
					label = n.Name
					if n.Dir {
						label += "/"
					} else {
						color = extensionColor(filepath.Ext(n.Name))
					}
				}
				p.Fill(ui.Rect{X: r.X + b.X, Y: r.Y + b.Y, W: max(b.W-1, 0), H: max(b.H-1, 0)}, color, 0)
				if b.ID == m.diskNode {
					p.Stroke(ui.Rect{X: r.X + b.X + 1, Y: r.Y + b.Y + 1, W: max(b.W-3, 0), H: max(b.H-3, 0)}, ui.RGB(255, 255, 255), 0, 2)
				}
				if b.W > 90 && b.H > 24 {
					maxChars := int((b.W - 8) / 7)
					runes := []rune(label)
					if len(runes) > maxChars && maxChars > 2 {
						label = string(runes[:maxChars-1]) + "…"
					}
					p.Clip(ui.Rect{X: r.X + b.X, Y: r.Y + b.Y, W: b.W, H: b.H}, 0, func() { p.Text(r.X+b.X+4, r.Y+b.Y+4, label, 11, ui.RGB(255, 255, 255)) })
				}
			}
		})
		ui.Text(c, m.mapHover).FillWidth().Height(24).Shrink(0).SingleLine().Padding(3, 10).FontSize(11).TextColor(t.TextMuted)
	})
}

type extensionStat struct {
	Extension    string
	Files, Bytes uint64
}

func fileExtension(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return "(no extension)"
	}
	return ext
}

func (m *Model) extensionTable(c *ui.Context) {
	cols := []ui.TableColumn{{Title: "Extension", Width: 100}, {Title: "Files", Width: 70, Align: ui.End}, {Title: "Size", Width: 95, Align: ui.End}}
	m.extensionList.Selected = &m.extensionSelected
	m.extensionList.Key = func(i int) any { return m.diskExtensions[i].Extension }
	table := ui.Table(c, &m.extensionList, cols, len(m.diskExtensions), func(i, col int) {
		s := m.diskExtensions[i]
		text := s.Extension
		if col == 1 {
			text = fmt.Sprint(s.Files)
		} else if col == 2 {
			text = format.Bytes(s.Bytes)
		}
		ui.Text(c, text).SingleLine()
	}).Fill().Label("File types")
	if table.Changed() && m.extensionSelected >= 0 && m.extensionSelected < len(m.diskExtensions) {
		m.extensionFilter = m.diskExtensions[m.extensionSelected].Extension
		m.diskMode = "Files"
		m.diskViewKey = ""
		m.diskSelected = -1
		m.diskNode = -1
	}
}
func extensionColor(ext string) ui.Color {
	palette := []ui.Color{ui.Hex("#3178aa"), ui.Hex("#267d69"), ui.Hex("#926839"), ui.Hex("#855a99"), ui.Hex("#aa5360"), ui.Hex("#526bb2")}
	h := uint32(2166136261)
	for _, ch := range strings.ToLower(ext) {
		h = (h ^ uint32(ch)) * 16777619
	}
	return palette[h%uint32(len(palette))]
}
