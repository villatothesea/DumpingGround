package app

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (m *Model) rebuildProcessTree() {
	m.processByPID = make(map[uint32]process.Info, len(m.processRows))
	m.processChildren = map[uint32][]uint32{}
	m.processRoots = m.processRoots[:0]
	m.processFlat = m.processFlat[:0]
	for _, r := range m.processRows {
		m.processByPID[r.PID] = r
	}
	query := strings.ToLower(strings.TrimSpace(m.processQuery))
	included := map[uint32]bool{}
	for _, r := range m.processRows {
		if query == "" || strings.Contains(strings.ToLower(r.Name+" "+r.Path), query) || strconv.FormatUint(uint64(r.PID), 10) == query {
			included[r.PID] = true
			m.processFlat = append(m.processFlat, r.PID)
		}
	}
	// Preserve ancestors of matches, with an explicit bound for stale/cyclic PPIDs.
	for _, id := range m.processFlat {
		seen := map[uint32]bool{id: true}
		p := m.processByPID[id].PPID
		for p != 0 && !seen[p] {
			r, ok := m.processByPID[p]
			if !ok {
				break
			}
			seen[p] = true
			included[p] = true
			p = r.PPID
		}
	}
	for _, r := range m.processRows {
		if !included[r.PID] {
			continue
		}
		parent, exists := m.processByPID[r.PPID]
		valid := exists && r.PPID != 0 && r.PPID != r.PID && included[r.PPID]
		if valid && !r.Started.IsZero() && !parent.Started.IsZero() && parent.Started.After(r.Started) {
			valid = false
		}
		seen := map[uint32]bool{r.PID: true}
		p := r.PPID
		for valid && p != 0 {
			if seen[p] {
				valid = false
				break
			}
			seen[p] = true
			a, ok := m.processByPID[p]
			if !ok {
				break
			}
			p = a.PPID
		}
		if valid {
			m.processChildren[r.PPID] = append(m.processChildren[r.PPID], r.PID)
		} else {
			m.processRoots = append(m.processRoots, r.PID)
		}
	}
	m.sortProcessTree()
	if query != "" {
		for id := range m.processChildren {
			m.processTree.Open.Add(id)
		}
	}
}
func (m *Model) sortProcessTree() {
	compare := func(a, b uint32) int {
		x, y := m.processByPID[a], m.processByPID[b]
		n := 0
		switch m.processSort.Column {
		case "pid":
			n = cmp.Compare(x.PID, y.PID)
		case "cpu":
			n = cmp.Compare(x.CPU, y.CPU)
		case "memory":
			n = cmp.Compare(x.Memory, y.Memory)
		case "private":
			n = cmp.Compare(x.Private, y.Private)
		case "read":
			n = cmp.Compare(x.ReadRate, y.ReadRate)
		case "write":
			n = cmp.Compare(x.WriteRate, y.WriteRate)
		case "threads":
			n = cmp.Compare(x.Threads, y.Threads)
		default:
			n = strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		}
		if n == 0 {
			n = cmp.Compare(a, b)
		}
		if m.processSort.Descending {
			return -n
		}
		return n
	}
	slices.SortFunc(m.processFlat, compare)
	slices.SortFunc(m.processRoots, compare)
	for p := range m.processChildren {
		slices.SortFunc(m.processChildren[p], compare)
	}
}
func (m *Model) selectedProcess() (process.Info, bool) {
	i := m.processSelected
	if m.processTreeMode {
		if i >= 0 && i < m.processTree.Rows() {
			r, ok := m.processByPID[m.processTree.Item(i)]
			return r, ok
		}
	} else if i >= 0 && i < len(m.processFlat) {
		r, ok := m.processByPID[m.processFlat[i]]
		return r, ok
	}
	return process.Info{}, false
}
func (m *Model) processView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Height(40).Shrink(0).PaddingX(12).Gap(8).AlignItems(ui.Center).Children(func() {
		m.segPair(c, t, "Process view", []string{"List", "Tree"}, b2i(m.processTreeMode), func(i int) {
			m.processTreeMode = i == 1
			m.processSelected = -1
		})
		pauseGlyph, pauseText := "pause", "Pause"
		if m.processPaused {
			pauseGlyph, pauseText = "play", "Resume"
		}
		if m.iconTextButton(c, pauseGlyph, pauseText).Clicked() {
			m.processPaused = !m.processPaused
		}
		if m.processPaused && m.iconTextButton(c, "refresh", "Refresh once").Clicked() {
			m.refreshOnce()
		}
		ui.Spacer(c)
		label := "Every " + m.processInterval
		if m.processPaused {
			label = "Paused"
		}
		if strings.TrimSpace(m.processQuery) != "" {
			label += fmt.Sprintf("  ·  %d of %d match", len(m.processFlat), len(m.processRows))
		}
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted).Shrink(0)
	})
	if m.lastProcessError != "" {
		ui.Text(c, m.lastProcessError).Padding(2, 12).TextColor(t.Danger).SingleLine()
	}
	columns := []ui.TableColumn{
		{ID: "name", Title: "Process", Width: 230, Sortable: true}, {ID: "pid", Title: "PID", Width: 70, Align: ui.End, Sortable: true},
		{ID: "cpu", Title: "CPU %", Width: 75, Align: ui.End, Sortable: true}, {ID: "memory", Title: "Working set", Width: 104, Align: ui.End, Sortable: true},
		{ID: "private", Title: "Private bytes", Width: 104, Align: ui.End, Sortable: true}, {ID: "read", Title: "Read /s", Width: 96, Align: ui.End, Sortable: true},
		{ID: "write", Title: "Write /s", Width: 96, Align: ui.End, Sortable: true}, {ID: "threads", Title: "Threads", Width: 74, Align: ui.End, Sortable: true},
	}
	cell := func(id uint32, col int) {
		r := m.processByPID[id]
		text := ""
		switch col {
		case 0:
			text = r.Name
		case 1:
			text = fmt.Sprint(r.PID)
		case 2:
			text = format.Percent(r.CPU)
		case 3:
			text = format.Bytes(r.Memory)
		case 4:
			text = format.Bytes(r.Private)
		case 5:
			text = format.Rate(r.ReadRate)
		case 6:
			text = format.Rate(r.WriteRate)
		case 7:
			text = fmt.Sprint(r.Threads)
		}
		if r.Access != "" && col >= 2 && col <= 6 {
			text = "—"
		}
		rowView := ui.Row(c).Gap(6).AlignItems(ui.Center)
		rowView.Children(func() {
			if col == 0 {
				if icon := m.processIcon(r.Path); icon != nil {
					ui.Image(c, icon).Size(16, 16)
				}
			}
			ui.Text(c, text).SingleLine().Tooltip(r.Path)
		})
		rowView.ContextMenu(func(menu *ui.Menu) {
			if m.processTreeMode {
				for i := 0; i < m.processTree.Rows(); i++ {
					if m.processTree.Item(i) == id {
						m.processSelected = i
						break
					}
				}
			} else {
				m.processSelected = slices.Index(m.processFlat, id)
			}
			m.processMenu(menu, r)
		})
	}
	table := func() {
		ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if m.processTreeMode {
				m.processTree.List.Selected = &m.processSelected
				m.processTree.List.Sort = &m.processSort
				ui.OutlineTable(c, &m.processTree, columns, m.processRoots, func(id uint32) []uint32 { return m.processChildren[id] }, cell).FillWidth().Grow(1).Label("Process table")
			} else {
				m.processList.Selected = &m.processSelected
				m.processList.Sort = &m.processSort
				m.processList.Key = func(i int) any {
					r := m.processByPID[m.processFlat[i]]
					return struct {
						PID     uint32
						Started int64
					}{r.PID, r.Started.UnixNano()}
				}
				if ui.Table(c, &m.processList, columns, len(m.processFlat), func(row, col int) { cell(m.processFlat[row], col) }).FillWidth().Grow(1).Label("Process table").Submitted() {
					m.showDetails = true
				}
			}
		})
	}
	selected, ok := m.selectedProcess()
	if m.showDetails && ok {
		m.requestInspection(selected.PID)
		ui.SplitVertical(c, &m.detailSplit, func() {
			ui.Column(c).Fill().Children(func() {
				table()
				m.processSelBar(c, selected, ok)
			})
		}, func() {
			m.processDetails(c)
		}).Grow(1).MinHeight(0).FillWidth()
	} else {
		ui.Column(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			table()
			m.processSelBar(c, selected, ok)
		})
	}
}

// processSelBar is the 36 DIP selection strip of the redesign: icon, name,
// PID and path on the left, actions on the right.
func (m *Model) processSelBar(c *ui.Context, selected process.Info, ok bool) {
	t := c.Theme()
	bar := ui.Row(c).FillWidth().Height(36).Shrink(0).PaddingX(12).Gap(8).AlignItems(ui.Center).Background(t.Surface)
	bar.BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	bar.Children(func() {
		if ok {
			if icon := m.processIcon(selected.Path); icon != nil {
				ui.Image(c, icon).Size(16, 16)
			}
			ui.Text(c, selected.Name).Bold().SingleLine().Shrink(0)
			ui.Text(c, fmt.Sprintf("PID %d", selected.PID)).TextColor(t.TextMuted).Shrink(0)
			path := selected.Path
			if path == "" {
				path = "(no path)"
			}
			if selected.Access != "" {
				path += " · " + selected.Access
			}
			ui.Text(c, path).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted).Tooltip(path)
		} else {
			ui.Text(c, "Select a process to inspect or act on").SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted)
		}
		if m.iconTextButton(c, "folder", "Open location").Disabled(!ok || selected.Path == "").Clicked() {
			mygo.Shell.ShowItemInFolder(selected.Path)
		}
		if m.iconTextButton(c, "copy", "Copy path").Disabled(!ok || selected.Path == "").Clicked() {
			mygo.Clipboard.WriteText(selected.Path)
		}
		if m.iconTextButton(c, "x", "End process").Disabled(!ok || selected.PID <= 4).TextColor(t.Danger).Clicked() {
			m.terminateTarget = selected
			m.terminateTree = false
			m.terminateOpen = true
		}
		m.iconButton(c, "more", "More end options").Disabled(!ok || selected.PID <= 4).Menu(func(menu *ui.Menu) {
			if menu.Item("End process tree").Chosen() {
				m.terminateTarget = selected
				m.terminateTree = true
				m.terminateOpen = true
			}
		})
		det := "chup"
		if m.showDetails {
			det = "down"
		}
		if m.iconTextButton(c, det, "Details").Clicked() {
			m.showDetails = !m.showDetails
		}
	})
}
func (m *Model) processIcon(path string) *ui.Bitmap {
	if icon, ok := m.iconCache[path]; ok {
		return icon
	}
	if path != "" && m.window != nil && len(m.iconCache) < 256 && !m.iconPending[path] && !strings.HasPrefix(path, `\\`) {
		select {
		case m.iconRequests <- path:
			m.iconPending[path] = true
		default:
		}
	}
	return m.iconCache[""]
}

func (m *Model) loadIcons() {
	for {
		select {
		case path := <-m.iconRequests:
			icon := systemIcon(path)
			m.post(func() {
				delete(m.iconPending, path)
				if icon == nil {
					icon = m.iconCache[""]
				}
				if len(m.iconCache) < 256 {
					m.iconCache[path] = icon
				}
			})
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Model) requestInspection(pid uint32) {
	if m.inspectedPID == pid {
		return
	}
	m.inspectedPID = pid
	m.inspectedModules = nil
	m.inspectedThreads = nil
	m.handles = nil
	m.stacks = nil
	m.handleErr = ""
	m.stackErr = ""
	m.inspectError = "Loading…"
	if m.window == nil {
		return
	}
	inspector, ok := m.processSvc.Inspect()
	if !ok {
		m.inspectError = "Unavailable on this platform"
		return
	}
	go func() {
		modules, e1 := inspector.Modules(pid)
		threads, e2 := inspector.Threads(pid)
		var info process.Info
		var e3 error
		if p, ok := inspector.(process.PropertyInspector); ok {
			info, e3 = p.Properties(pid)
		}
		r := inspectionResult{pid: pid, modules: modules, threads: threads, err: errors.Join(e1, e2, e3), info: info, stacks: map[uint32][]process.StackFrame{}}
		if s, ok := inspector.(process.StackInspector); ok && len(threads) > 0 {
			r.stacks[threads[0].ID], r.stackErr = s.Stack(pid, threads[0].ID, 64)
		}
		select {
		case <-m.inspection:
		default:
		}
		select {
		case m.inspection <- r:
			if m.window != nil {
				m.window.Invalidate()
			}
		case <-m.ctx.Done():
		}
	}()
}

// segPair is a two/three-option segmented control built from clickable rows:
// unlike ui.Segmented, whose choice lands after the frame, e.Clicked() applies
// in the same frame so derived state stays consistent.
func (m *Model) segPair(c *ui.Context, t *ui.Theme, name string, labels []string, sel int, choose func(int)) {
	g := ui.Row(c).Height(28).Radius(6).Shrink(0).Label(name)
	g.BorderWidth(1).BorderColor(t.Border)
	g.Children(func() {
		for i, name := range labels {
			i, name := i, name
			on := sel == i
			e := ui.Row(c).Height(26).PaddingX(10).AlignItems(ui.Center).Radius(5).Label(name)
			e.Draw(func(p *ui.Painter, r ui.Rect) {
				if on {
					p.Fill(r, t.SurfacePressed, 5)
				} else if e.Hovered() {
					p.Fill(r, t.SurfaceHover, 5)
				}
			})
			e.Children(func() {
				tx := ui.Text(c, name).SingleLine()
				if on {
					tx.Bold()
				} else {
					tx.TextColor(t.TextMuted)
				}
			})
			if e.Clicked() && !on {
				choose(i)
			}
		}
	})
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

var detailTabs = []string{"Properties", "Modules", "Threads", "Handles", "Stack"}

// detailPill is one dock tab: a small pill rather than ui.Tabs so the click
// applies in the same frame.
func (m *Model) detailPill(c *ui.Context, t *ui.Theme, name string) {
	on := m.detailTab == name
	e := ui.Row(c).Height(24).PaddingX(10).AlignItems(ui.Center).Radius(12).Label(name)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if on {
			p.Fill(r, t.SurfacePressed, 12)
		} else if e.Hovered() {
			p.Fill(r, t.SurfaceHover, 12)
		}
	})
	e.Children(func() {
		tx := ui.Text(c, name).SingleLine()
		if on {
			tx.Bold()
		} else {
			tx.TextColor(t.TextMuted)
		}
	})
	if e.Clicked() {
		m.detailTab = name
	}
}

func (m *Model) processDetails(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
		head := ui.Row(c).Height(32).Shrink(0).PaddingX(8).Gap(8).AlignItems(ui.Center)
		head.BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
		head.Children(func() {
			ui.Row(c).Gap(2).Children(func() {
				for _, name := range detailTabs {
					m.detailPill(c, t, name)
				}
			})
			ui.Text(c, fmt.Sprintf("%d modules · %d threads", len(m.inspectedModules), len(m.inspectedThreads))).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, m.inspectError).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted)
			if m.iconButton(c, "x", "Close details").Clicked() {
				m.showDetails = false
			}
		})
		if m.detailTab == "Properties" {
			i := m.inspectedInfo
			ui.Column(c).Grow(1).Padding(4, 10).Gap(4).Children(func() {
				ui.Text(c, fmt.Sprintf("User: %s   ·   Elevated: %t   ·   Handles: %d   ·   Priority class: 0x%x", i.User, i.Elevated, i.Handles, i.Priority)).SingleLine()
				ui.Text(c, i.Path).SingleLine().Tooltip(i.Path)
				ui.Text(c, i.Command).SingleLine().Tooltip(i.Command)
				if ui.Button(c, "Copy command line").Disabled(i.Command == "").Clicked() {
					mygo.Clipboard.WriteText(i.Command)
				}
				if i.Access != "" {
					ui.Text(c, i.Access).SingleLine().TextColor(c.Theme().TextMuted)
				}
			})
		} else if m.detailTab == "Modules" {
			cols := []ui.TableColumn{{Title: "Module", Width: 200}, {Title: "Image size", Width: 100}, {Title: "Path"}}
			ui.Table(c, &m.detailList, cols, len(m.inspectedModules), func(i, col int) {
				r := m.inspectedModules[i]
				s := r.Name
				if col == 1 {
					s = format.Bytes(r.Size)
				} else if col == 2 {
					s = r.Path
				}
				ui.Text(c, s).SingleLine().Tooltip(r.Path)
			}).Grow(1).FillWidth()
		} else if m.detailTab == "Threads" {
			cols := []ui.TableColumn{{Title: "Thread ID", Width: 140}, {Title: "Owner PID", Width: 140}, {Title: "Base priority"}}
			ui.Table(c, &m.detailList, cols, len(m.inspectedThreads), func(i, col int) {
				r := m.inspectedThreads[i]
				s := fmt.Sprint(r.ID)
				if col == 1 {
					s = fmt.Sprint(r.OwnerPID)
				} else if col == 2 {
					s = fmt.Sprint(r.Priority)
				}
				ui.Text(c, s)
			}).Grow(1).FillWidth()
		} else if m.detailTab == "Handles" {
			ui.Row(c).Height(28).Shrink(0).PaddingX(10).Gap(6).Children(func() {
				ui.SearchField(c, &m.handleQuery).Placeholder("Filter type or name").Width(220)
				if ui.Button(c, "Search handles").Clicked() {
					m.requestHandleSearch(m.inspectedPID)
				}
				ui.Text(c, fmt.Sprintf("%d results", len(m.handles))).TextColor(c.Theme().TextMuted)
			})
			cols := []ui.TableColumn{{Title: "Handle", Width: 90}, {Title: "Type", Width: 130}, {Title: "Name"}, {Title: "Access", Width: 90, Align: ui.End}}
			ui.Table(c, &m.detailList, cols, len(m.handles), func(i, col int) {
				h := m.handles[i]
				s := fmt.Sprintf("0x%x", h.Value)
				if col == 1 {
					s = h.Type
				} else if col == 2 {
					s = h.Name
				} else if col == 3 {
					s = fmt.Sprintf("0x%x", h.Access)
				}
				ui.Text(c, s).SingleLine().Tooltip(h.Name)
			}).Grow(1).FillWidth()
			if m.handleErr != "" {
				ui.Text(c, m.handleErr).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
			}
		} else {
			threadID := m.stackThread
			if threadID == 0 && len(m.inspectedThreads) > 0 {
				threadID = m.inspectedThreads[0].ID
			}
			frames := m.stacks[threadID]
			ui.Row(c).Height(28).Shrink(0).PaddingX(10).Gap(6).Children(func() {
				ui.Text(c, fmt.Sprintf("Thread %d · %d frames", threadID, len(frames)))
				if ui.Button(c, "Refresh stack").Clicked() {
					m.requestStack(m.inspectedPID, threadID)
				}
			})
			cols := []ui.TableColumn{{Title: "Address", Width: 150}, {Title: "Symbol"}, {Title: "Offset", Width: 100, Align: ui.End}}
			ui.Table(c, &m.detailList, cols, len(frames), func(i, col int) {
				f := frames[i]
				s := fmt.Sprintf("0x%x", f.Address)
				if col == 1 {
					s = f.Symbol
					if s == "" {
						s = "(unresolved)"
					}
				} else if col == 2 {
					s = fmt.Sprintf("0x%x", f.Offset)
				}
				ui.Text(c, s).SingleLine()
			}).Grow(1).FillWidth()
			if m.stackErr != "" {
				ui.Text(c, m.stackErr).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
			}
		}
	})
}
