package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/dumpingground/dumpingground/internal/treemap"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type page uint8

const (
	processPage page = iota
	diskPage
)

type Model struct {
	mu               sync.Mutex
	window           *mygo.Window
	processSvc       *process.Service
	processRows      []process.Info
	processByPID     map[uint32]process.Info
	processChildren  map[uint32][]uint32
	processRoots     []uint32
	processTree      ui.OutlineState[uint32]
	processSelected  int
	processQuery     string
	processSort      ui.SortOrder
	inspectedPID     uint32
	inspectedModules []process.Module
	inspectedThreads []process.Thread
	inspectError     string
	page             page
	lastProcessError string

	diskScanner  disk.Scanner
	diskPath     string
	diskSnapshot *disk.Snapshot
	diskRoot     int
	diskSelected int
	diskProgress disk.Progress
	diskStatus   string
	diskCancel   context.CancelFunc
	diskItems    []treemap.Item
}

func New() *Model {
	path, _ := os.Getwd()
	return &Model{
		processSvc:   process.NewService(),
		processByPID: make(map[uint32]process.Info),
		diskPath:     path,
		diskStatus:   "Choose a folder and start a scan",
	}
}

func (m *Model) Run() error {
	mygo.App.WhenReady(func() {
		if !mygo.App.RequestSingleInstanceLock() {
			mygo.App.Quit()
			return
		}
		m.window = mygo.NewWindow(mygo.WindowOptions{
			Title: "DumpingGround",
			Width: 1280, Height: 800, MinWidth: 920, MinHeight: 600,
			StateKey: "main", Content: ui.View(m.view),
		})
		go m.refreshProcesses()
	})
	return mygo.App.Run()
}

func (m *Model) refreshProcesses() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		rows, err := m.processSvc.Snapshot()
		if m.window != nil {
			m.window.Update(func() {
				m.processRows = rows
				m.lastProcessError = ""
				if err != nil {
					m.lastProcessError = err.Error()
				}
				m.rebuildProcessTree()
			})
		}
		<-ticker.C
	}
}

func (m *Model) rebuildProcessTree() {
	m.processByPID = make(map[uint32]process.Info, len(m.processRows))
	m.processChildren = make(map[uint32][]uint32)
	m.processRoots = m.processRoots[:0]
	for _, row := range m.processRows {
		m.processByPID[row.PID] = row
	}
	query := strings.ToLower(strings.TrimSpace(m.processQuery))
	included := make(map[uint32]bool, len(m.processRows))
	for _, row := range m.processRows {
		included[row.PID] = query == "" || strings.Contains(strings.ToLower(row.Name), query) || strings.Contains(strings.ToLower(row.Path), query)
	}
	for _, row := range m.processRows {
		if !included[row.PID] {
			continue
		}
		for parent := row.PPID; ; {
			if parent == 0 || parent == row.PID {
				break
			}
			ancestor, ok := m.processByPID[parent]
			if !ok {
				break
			}
			included[ancestor.PID] = true
			if ancestor.PPID == ancestor.PID {
				break
			}
			parent = ancestor.PPID
		}
	}
	for _, row := range m.processRows {
		if !included[row.PID] {
			continue
		}
		if _, ok := m.processByPID[row.PPID]; ok && row.PPID != row.PID && included[row.PPID] {
			m.processChildren[row.PPID] = append(m.processChildren[row.PPID], row.PID)
		} else {
			m.processRoots = append(m.processRoots, row.PID)
		}
	}
	for parent := range m.processChildren {
		sort.Slice(m.processChildren[parent], func(i, j int) bool {
			return m.processByPID[m.processChildren[parent][i]].Name < m.processByPID[m.processChildren[parent][j]].Name
		})
	}
	sort.Slice(m.processRoots, func(i, j int) bool {
		return m.processByPID[m.processRoots[i]].Name < m.processByPID[m.processRoots[j]].Name
	})
}

func (m *Model) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		m.toolbar(c)
		ui.Row(c).Grow(1).Children(func() {
			m.navigation(c)
			ui.Column(c).Grow(1).Padding(16).Gap(12).Children(func() {
				if m.page == processPage {
					m.processView(c)
				} else {
					m.diskView(c)
				}
			})
		})
		ui.Row(c).Height(28).Shrink(0).PaddingX(12).AlignItems(ui.Center).Background(t.Surface).Children(func() {
			status := "Process sampler: active"
			if m.page == diskPage {
				status = m.diskStatus
			}
			ui.Text(c, status).FontSize(12).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.Text(c, "DumpingGround 0.1 • native UI").FontSize(12).TextColor(t.TextMuted)
		})
	})
}

func (m *Model) toolbar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Height(56).Shrink(0).Padding(12, 16).Gap(10).AlignItems(ui.Center).Background(t.Surface).Children(func() {
		ui.Text(c, "DumpingGround").FontSize(20).Bold()
		ui.Text(c, "system observatory").FontSize(12).TextColor(t.TextMuted)
		ui.Spacer(c)
		if m.page == processPage {
			ui.SearchField(c, &m.processQuery).Placeholder("Search processes").Width(240)
			if ui.Button(c, "Refresh").Clicked() {
				go m.refreshOnce()
			}
		} else {
			ui.TextInput(c, &m.diskPath).Placeholder("Folder to scan").Width(300)
			if ui.PrimaryButton(c, "Scan").Clicked() {
				m.startScan()
			}
			if ui.Button(c, "Cancel").Disabled(m.diskCancel == nil).Clicked() {
				m.cancelScan()
			}
		}
	})
}

func (m *Model) navigation(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Width(156).Shrink(0).Padding(12, 10).Gap(6).Background(t.Surface).Children(func() {
		ui.Text(c, "EXPLORE").FontSize(11).TextColor(t.TextMuted).Padding(6, 8)
		p := ui.Button(c, "◉  Processes").Width(136)
		if m.page == processPage {
			p = p.Background(t.Accent).TextColor(t.AccentText)
		}
		if p.Clicked() {
			m.page = processPage
		}
		d := ui.Button(c, "▦  Disk map").Width(136)
		if m.page == diskPage {
			d = d.Background(t.Accent).TextColor(t.AccentText)
		}
		if d.Clicked() {
			m.page = diskPage
		}
		ui.Spacer(c)
		ui.Text(c, "LOW FOOTPRINT").FontSize(10).TextColor(t.TextMuted).Padding(6, 8)
		ui.Text(c, "Background work is paused or reduced when it is not needed.").FontSize(11).TextColor(t.TextMuted).Padding(6, 8)
	})
}

func (m *Model) processView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Grow(1).Gap(12).Children(func() {
		ui.Column(c).Grow(1).MinWidth(500).Gap(8).Children(func() {
			ui.Row(c).Height(30).Shrink(0).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("Processes  ·  %d", len(m.processRows))).Bold()
				ui.Spacer(c)
				if m.lastProcessError != "" {
					ui.Text(c, m.lastProcessError).FontSize(12).TextColor(t.TextMuted)
				}
			})
			m.processTable(c)
		})
		m.processDetails(c)
	})
}

func (m *Model) processTable(c *ui.Context) {
	cols := []ui.TableColumn{
		{ID: "name", Title: "Process", Width: 260, Sortable: true},
		{ID: "pid", Title: "PID", Width: 76, Align: ui.End, Sortable: true},
		{ID: "cpu", Title: "CPU", Width: 76, Align: ui.End, Sortable: true},
		{ID: "memory", Title: "Memory", Width: 100, Align: ui.End, Sortable: true},
		{ID: "io", Title: "I/O", Width: 120, Align: ui.End, Sortable: true},
	}
	children := func(pid uint32) []uint32 { return m.processChildren[pid] }
	cell := func(pid uint32, col int) {
		row := m.processByPID[pid]
		switch col {
		case 0:
			ui.Text(c, row.Name).SingleLine()
		case 1:
			ui.Text(c, fmt.Sprint(row.PID)).SingleLine()
		case 2:
			ui.Text(c, format.Percent(row.CPU)).SingleLine()
		case 3:
			ui.Text(c, format.Bytes(row.Memory)).SingleLine()
		case 4:
			ui.Text(c, format.Bytes(row.ReadBytes+row.WriteBytes)).SingleLine()
		}
	}
	if len(m.processRoots) == 0 {
		ui.Column(c).Grow(1).Center().Children(func() { ui.Text(c, "No process snapshot yet").TextColor(c.Theme().TextMuted) })
		return
	}
	m.processTree.List.Selected = &m.processSelected
	m.processTree.List.Sort = &m.processSort
	m.processTree.List.Label = func(i int) string { return m.processByPID[m.processTree.Item(i)].Name }
	m.sortProcessTree()
	ui.OutlineTable(c, &m.processTree, cols, m.processRoots, children, cell).Grow(1)
}

func (m *Model) sortProcessTree() {
	compare := func(a, b uint32) bool {
		x, y := m.processByPID[a], m.processByPID[b]
		cmp := 0
		switch m.processSort.Column {
		case "pid":
			cmp = compareUint64(uint64(x.PID), uint64(y.PID))
		case "cpu":
			if x.CPU < y.CPU {
				cmp = -1
			} else if x.CPU > y.CPU {
				cmp = 1
			}
		case "memory":
			cmp = compareUint64(x.Memory, y.Memory)
		case "io":
			cmp = compareUint64(x.ReadBytes+x.WriteBytes, y.ReadBytes+y.WriteBytes)
		default:
			cmp = strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		}
		if cmp == 0 {
			cmp = compareUint64(uint64(x.PID), uint64(y.PID))
		}
		if m.processSort.Descending {
			return cmp > 0
		}
		return cmp < 0
	}
	sort.SliceStable(m.processRoots, func(i, j int) bool { return compare(m.processRoots[i], m.processRoots[j]) })
	for parent := range m.processChildren {
		sort.SliceStable(m.processChildren[parent], func(i, j int) bool { return compare(m.processChildren[parent][i], m.processChildren[parent][j]) })
	}
}

func compareUint64(a, b uint64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (m *Model) processDetails(c *ui.Context) {
	t := c.Theme()
	var selected process.Info
	if m.processSelected >= 0 && m.processSelected < m.processTree.Rows() {
		selected = m.processByPID[m.processTree.Item(m.processSelected)]
	}
	ui.Column(c).Width(280).Shrink(0).Padding(14).Gap(8).Background(t.Surface).Children(func() {
		ui.Text(c, "DETAILS").FontSize(11).TextColor(t.TextMuted)
		if selected.PID == 0 {
			ui.Text(c, "Select a process to inspect its path, memory and actions.").TextColor(t.TextMuted)
			return
		}
		m.requestInspection(selected.PID)
		ui.Text(c, selected.Name).FontSize(18).Bold().SingleLine()
		ui.Text(c, fmt.Sprintf("PID %d", selected.PID)).TextColor(t.TextMuted)
		ui.Text(c, "Memory").FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, format.Bytes(selected.Memory)).FontSize(15)
		ui.Text(c, "CPU").FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, format.Percent(selected.CPU)).FontSize(15)
		ui.Text(c, selected.Path).FontSize(12).SingleLine().Tooltip(selected.Path)
		ui.Text(c, fmt.Sprintf("Threads  %d   Modules  %d", len(m.inspectedThreads), len(m.inspectedModules))).FontSize(12).TextColor(t.TextMuted)
		if m.inspectError != "" {
			ui.Text(c, m.inspectError).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}
		if len(m.inspectedModules) > 0 {
			ui.Text(c, "Modules").FontSize(11).TextColor(t.TextMuted)
			for i := 0; i < len(m.inspectedModules) && i < 3; i++ {
				ui.Text(c, m.inspectedModules[i].Name).FontSize(11).SingleLine()
			}
		}
		if len(m.inspectedThreads) > 0 {
			ui.Text(c, fmt.Sprintf("Threads: %d", len(m.inspectedThreads))).FontSize(11).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		if ui.Button(c, "Terminate process").Clicked() {
			go m.terminate(selected.PID, false)
		}
		if ui.Button(c, "Terminate process tree").Clicked() {
			go m.terminate(selected.PID, true)
		}
	})
}

func (m *Model) diskView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Height(32).Shrink(0).AlignItems(ui.Center).Children(func() {
		if m.diskSnapshot != nil && m.diskRoot != m.diskSnapshot.Root && ui.Button(c, "← Back").Clicked() {
			m.diskRoot = m.diskSnapshot.Nodes[m.diskRoot].Parent
		}
		if m.diskSnapshot != nil {
			ui.Text(c, "  "+m.diskSnapshot.Nodes[m.diskRoot].Path).TextColor(t.TextMuted).SingleLine()
		}
	})
	if m.diskSnapshot == nil {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Text(c, "Disk space map").FontSize(26).Bold()
			ui.Text(c, "Scan a folder when you need it. The index is released when you clear it.").TextColor(t.TextMuted)
			if ui.PrimaryButton(c, "Scan current folder").Clicked() {
				m.startScan()
			}
		})
		return
	}
	ui.Row(c).Grow(1).Gap(12).Children(func() {
		ui.Column(c).Width(330).Shrink(0).Gap(8).Children(func() {
			ui.Text(c, fmt.Sprintf("logical %s · allocated %s · %s", format.Bytes(m.diskSnapshot.Bytes), format.Bytes(m.diskSnapshot.Allocated), m.diskStatus)).FontSize(12).TextColor(t.TextMuted)
			m.diskTable(c)
		})
		m.diskMap(c)
	})
}

func (m *Model) diskTable(c *ui.Context) {
	root := m.diskSnapshot.Nodes[m.diskRoot]
	cols := []ui.TableColumn{{Title: "Name", Width: 210}, {Title: "Size", Width: 100, Align: ui.End}}
	items := append([]int(nil), root.Children...)
	state := &ui.ListState{Selected: &m.diskSelected}
	table := ui.Table(c, state, cols, len(items), func(row, col int) {
		n := m.diskSnapshot.Nodes[items[row]]
		if col == 0 {
			ui.Text(c, n.Name).SingleLine()
		} else {
			ui.Text(c, format.Bytes(nodeSize(n))).SingleLine()
		}
	}).Grow(1)
	if table.Submitted() && m.diskSelected >= 0 && m.diskSelected < len(items) && m.diskSnapshot.Nodes[items[m.diskSelected]].Dir {
		m.diskRoot = items[m.diskSelected]
		m.diskSelected = 0
	}
}

func (m *Model) diskMap(c *ui.Context) {
	root := m.diskSnapshot.Nodes[m.diskRoot]
	items := m.diskItems[:0]
	for _, id := range root.Children {
		n := m.diskSnapshot.Nodes[id]
		items = append(items, treemap.Item{ID: id, Label: n.Name, Value: nodeSize(n)})
	}
	m.diskItems = items
	t := c.Theme()
	panel := ui.Box(c).Grow(1).MinHeight(300).Background(t.Surface).Radius(8).Clip()
	bounds := panel.Bounds()
	width, height := bounds.W-2, bounds.H-2
	if width <= 0 {
		width = 640
	}
	if height <= 0 {
		height = 320
	}
	rects := treemap.Layout(items, width, height)
	panel.Children(func() {
		for i, rect := range rects {
			id := rect.ID
			if rect.W < 2 || rect.H < 2 {
				continue
			}
			color := []ui.Color{ui.Hex("#496A81"), ui.Hex("#6B8F71"), ui.Hex("#9B6B6B"), ui.Hex("#8267A8"), ui.Hex("#B08B57")}[i%5]
			item := ui.Box(c).Absolute().Left(rect.X + 1).Top(rect.Y + 1).Width(maxf(rect.W-2, 1)).Height(maxf(rect.H-2, 1)).Radius(5).Background(color).OnClick(func() {
				n := m.diskSnapshot.Nodes[id]
				if n.Dir {
					m.diskRoot, m.diskSelected = id, 0
				} else {
					m.diskStatus = fmt.Sprintf("%s · %s", n.Name, format.Bytes(nodeSize(n)))
				}
			})
			if rect.W > 46 && rect.H > 24 {
				item.Children(func() {
					ui.Text(c, m.diskSnapshot.Nodes[id].Name).FontSize(11).TextColor(ui.RGB(255, 255, 255)).SingleLine().Padding(6, 6)
				})
			}
		}
	})
}

func nodeSize(n disk.Node) uint64 {
	if n.Allocated > 0 {
		return n.Allocated
	}
	return n.Bytes
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func (m *Model) requestInspection(pid uint32) {
	if m.inspectedPID == pid {
		return
	}
	m.inspectedPID, m.inspectedModules, m.inspectedThreads, m.inspectError = pid, nil, nil, "Loading modules and threads…"
	inspector, ok := m.processSvc.Inspect()
	if !ok {
		m.inspectError = "Detailed inspection is unavailable on this platform"
		return
	}
	go func() {
		modules, moduleErr := inspector.Modules(pid)
		threads, threadErr := inspector.Threads(pid)
		if m.window != nil {
			m.window.Update(func() {
				if m.inspectedPID != pid {
					return
				}
				m.inspectedModules, m.inspectedThreads = modules, threads
				if moduleErr != nil {
					m.inspectError = moduleErr.Error()
				} else if threadErr != nil {
					m.inspectError = threadErr.Error()
				} else {
					m.inspectError = ""
				}
			})
		}
	}()
}

func (m *Model) refreshOnce() {
	rows, err := m.processSvc.Snapshot()
	if m.window != nil {
		m.window.Update(func() {
			m.processRows = rows
			if err != nil {
				m.lastProcessError = err.Error()
			}
			m.rebuildProcessTree()
		})
	}
}

func (m *Model) terminate(pid uint32, tree bool) {
	err := m.processSvc.Terminate(pid, tree)
	if m.window != nil {
		m.window.Update(func() {
			if err != nil {
				m.lastProcessError = err.Error()
			} else {
				m.lastProcessError = "Process termination requested"
			}
		})
	}
}

func (m *Model) startScan() {
	if m.diskCancel != nil {
		return
	}
	path := strings.TrimSpace(m.diskPath)
	if path == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.diskCancel = cancel
	m.diskSnapshot = nil
	m.diskStatus = "Scanning…"
	progress := make(chan disk.Progress, 8)
	go func() {
		snapshot, err := m.diskScanner.Scan(ctx, path, progress)
		close(progress)
		if m.window != nil {
			m.window.Update(func() {
				m.diskCancel = nil
				if err != nil {
					if errors.Is(err, context.Canceled) {
						m.diskStatus = "Scan cancelled"
					} else {
						m.diskStatus = err.Error()
					}
					return
				}
				m.diskSnapshot, m.diskRoot, m.diskStatus = snapshot, snapshot.Root, "Scan complete"
			})
		}
	}()
	go func() {
		for p := range progress {
			if m.window != nil {
				m.window.Update(func() {
					m.diskProgress = p
					m.diskStatus = fmt.Sprintf("Scanning… %d files · %s", p.Files, format.Bytes(p.Bytes))
				})
			}
		}
	}()
}

func (m *Model) cancelScan() {
	if m.diskCancel != nil {
		m.diskCancel()
		m.diskStatus = "Cancelling…"
	}
}
