package app

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

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
		// The shell launches apps but does not own them: nesting everything
		// started from Explorer under explorer.exe both hides the tree's real
		// shape and makes "End process tree" on the shell wipe out every app
		// the user opened. Task Manager shows no such level either, so the
		// shell's children stand as roots.
		if valid && strings.EqualFold(parent.Name, "explorer.exe") {
			valid = false
		}
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
	m.aggregateProcessTree()
	m.sortProcessTree()
	if query != "" {
		for id := range m.processChildren {
			m.processTree.Open.Add(id)
		}
	}
}

// aggregateProcessTree sums each subtree's additive columns into its root:
// in tree mode a row carries its own plus its descendants' CPU, memory, I/O
// and threads, so collapsed parents — and the top-level rows together —
// still total the system figures.
func (m *Model) aggregateProcessTree() {
	m.processAgg = make(map[uint32]process.Info, len(m.processByPID))
	active := make(map[uint32]bool, len(m.processByPID))
	var acc func(id uint32) process.Info
	acc = func(id uint32) process.Info {
		if r, ok := m.processAgg[id]; ok {
			return r
		}
		if active[id] {
			return process.Info{}
		}
		active[id] = true
		r := m.processByPID[id]
		for _, ch := range m.processChildren[id] {
			c := acc(ch)
			r.CPU += c.CPU
			r.Memory += c.Memory
			r.Private += c.Private
			r.ReadRate += c.ReadRate
			r.WriteRate += c.WriteRate
			r.Threads += c.Threads
		}
		delete(active, id)
		m.processAgg[id] = r
		return r
	}
	for _, id := range m.processRoots {
		acc(id)
	}
}

// displayProcess returns the row to show for id: its own metrics in list
// mode, the subtree's totals in tree mode.
func (m *Model) displayProcess(id uint32) process.Info {
	if m.processTreeMode {
		if r, ok := m.processAgg[id]; ok {
			return r
		}
	}
	return m.processByPID[id]
}

// processDir is the folder containing the process image, empty when the
// path is unavailable or carries no directory part.
func processDir(path string) string {
	path = strings.TrimRight(path, `/\`)
	switch i := strings.LastIndexAny(path, `/\`); {
	case i < 0:
		return ""
	case i == 2 && path[1] == ':': // a file at a drive root: C:\x.exe → C:\
		return path[:3]
	default:
		return path[:i]
	}
}

func (m *Model) sortProcessTree() {
	compare := func(a, b uint32) int {
		x, y := m.displayProcess(a), m.displayProcess(b)
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
		case "user":
			n = strings.Compare(strings.ToLower(x.User), strings.ToLower(y.User))
		case "dir":
			n = strings.Compare(strings.ToLower(processDir(x.Path)), strings.ToLower(processDir(y.Path)))
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

// processDescendants counts the processes nested under pid in the tree, so
// a destructive action can state its blast radius.
func (m *Model) processDescendants(pid uint32) int {
	stack := append([]uint32(nil), m.processChildren[pid]...)
	seen := map[uint32]bool{}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[p] {
			continue
		}
		seen[p] = true
		stack = append(stack, m.processChildren[p]...)
	}
	return len(seen)
}

// selectProcessRow makes pid the selected row in whichever mode is active.
func (m *Model) selectProcessRow(pid uint32) {
	if m.processTreeMode {
		for i := 0; i < m.processTree.Rows(); i++ {
			if m.processTree.Item(i) == pid {
				m.processSelected = i
				return
			}
		}
	} else {
		m.processSelected = slices.Index(m.processFlat, pid)
	}
}

// rankProcessColumns ranks the visible rows' values per metric column so
// cells can tint the ten largest: rank 1 in the accent's full strength,
// rank 10 faded toward the background.
func (m *Model) rankProcessColumns() {
	m.procTop = nil
	var ids []uint32
	if m.processTreeMode {
		// Only the rows on screen compete: collapsed subtrees would spend
		// the ten slots on ancestors that share a root's aggregate, leaving
		// the visible rows almost bare.
		if n := m.processTree.Rows(); n > 0 {
			ids = make([]uint32, 0, n)
			for i := 0; i < n; i++ {
				ids = append(ids, m.processTree.Item(i))
			}
		}
	} else {
		ids = slices.Clone(m.processFlat)
	}
	if len(ids) < 2 {
		return
	}
	metrics := []struct {
		col int
		val func(process.Info) float64
	}{
		{2, func(r process.Info) float64 { return r.CPU }},
		{3, func(r process.Info) float64 { return float64(r.Memory) }},
		{4, func(r process.Info) float64 { return float64(r.Private) }},
		{5, func(r process.Info) float64 { return float64(r.ReadRate) }},
		{6, func(r process.Info) float64 { return float64(r.WriteRate) }},
		{7, func(r process.Info) float64 { return float64(r.Threads) }},
	}
	for _, mt := range metrics {
		slices.SortFunc(ids, func(a, b uint32) int {
			return cmp.Compare(mt.val(m.displayProcess(b)), mt.val(m.displayProcess(a)))
		})
		var rank map[uint32]int
		for i := 0; i < len(ids) && i < 10; i++ {
			if mt.val(m.displayProcess(ids[i])) <= 0 {
				break
			}
			if rank == nil {
				rank = map[uint32]int{}
			}
			rank[ids[i]] = i + 1
		}
		if rank != nil {
			if m.procTop == nil {
				m.procTop = map[int]map[uint32]int{}
			}
			m.procTop[mt.col] = rank
		}
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
	m.procMenuHits = m.procMenuHits[:0]
	m.rankProcessColumns()
	selPID := uint32(0)
	if r, ok := m.selectedProcess(); ok {
		selPID = r.PID
	}
	ui.Row(c).FillWidth().Height(40).Shrink(0).PaddingX(12).Gap(8).AlignItems(ui.Center).Children(func() {
		m.modeButton(c, t, "Tree", m.processTreeMode, func() {
			m.processTreeMode = true
			m.processSelected = -1
		})
		m.modeButton(c, t, "List", !m.processTreeMode, func() {
			m.processTreeMode = false
			m.processSelected = -1
		})
		ui.Spacer(c)
		if strings.TrimSpace(m.processQuery) != "" {
			label := fmt.Sprintf("%s of %s match", format.Int(uint64(len(m.processFlat))), format.Int(uint64(len(m.processRows))))
			ui.Text(c, label).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		}
		m.selActs(c, t)
	})
	if m.lastProcessError != "" {
		ui.Text(c, m.lastProcessError).Padding(2, 12).TextColor(t.Danger).SingleLine()
	}
	columns := []ui.TableColumn{
		{ID: "name", Title: "Process", Width: 230, Sortable: true}, {ID: "pid", Title: "PID", Width: 70, Align: ui.End, Sortable: true},
		{ID: "cpu", Title: "CPU %", Width: 75, Align: ui.End, Sortable: true}, {ID: "memory", Title: "Working set", Width: 104, Align: ui.End, Sortable: true},
		{ID: "private", Title: "Private bytes", Width: 104, Align: ui.End, Sortable: true}, {ID: "read", Title: "Read /s", Width: 96, Align: ui.End, Sortable: true},
		{ID: "write", Title: "Write /s", Width: 96, Align: ui.End, Sortable: true}, {ID: "threads", Title: "Threads", Width: 74, Align: ui.End, Sortable: true},
		{ID: "user", Title: "User", Width: 180, Sortable: true},
		{ID: "dir", Title: "Directory", MinWidth: 260, Sortable: true},
	}
	cell := func(id uint32, col int) {
		r := m.displayProcess(id)
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
			text = format.Int(uint64(r.Threads))
		case 8:
			text = r.User
		case 9:
			text = processDir(r.Path)
		}
		if r.Access != "" && col >= 2 && col <= 6 {
			text = "—"
		}
		rowView := ui.Row(c).Gap(6).AlignItems(ui.Center)
		if col == 0 {
			if b := rowView.Bounds(); b.H > 0 {
				m.procMenuHits = append(m.procMenuHits, menuHit{y1: b.Y, y2: b.Y + b.H, id: int(id)})
			}
		}
		if col >= 2 && col <= 7 && r.Access == "" && id != selPID {
			if rank := m.procTop[col][id]; rank > 0 {
				// Task Manager style: the whole cell tints theme blue,
				// deepest at the column's top value, faint by rank ten.
				heat := t.Accent.Mix(t.Background, 1-(0.38-0.33*float32(rank-1)/9))
				pv, ph := t.Space(1.5), t.Space(2.5)
				rowView.Fill().Justify(ui.End).Draw(func(p *ui.Painter, r ui.Rect) {
					p.Fill(ui.Rect{X: r.X - ph, Y: r.Y - pv, W: r.W + 2*ph, H: r.H + 2*pv}, heat, 0)
				})
			}
		}
		rowView.Children(func() {
			if col == 0 {
				if icon := m.processIcon(r.Path); icon != nil {
					ui.Image(c, icon).Size(16, 16)
				}
			}
			ui.Text(c, text).SingleLine().Tooltip(r.Path)
		})
	}
	table := func() {
		ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if m.processTreeMode {
				m.processTree.List.Selected = &m.processSelected
				m.processTree.List.Sort = &m.processSort
				tbl := tableChrome(c, func() ui.Element {
					return ui.OutlineTable(c, &m.processTree, columns, m.processRoots, func(id uint32) []uint32 { return m.processChildren[id] }, cell)
				}).FillWidth().Grow(1).Label("Process table")
				tableRowMenu(tbl, &m.procMenuHits, func(id int, menu *ui.Menu) {
					m.selectProcessRow(uint32(id))
					m.processMenu(menu, m.processByPID[uint32(id)])
				})
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
				tbl := tableChrome(c, func() ui.Element {
					return ui.Table(c, &m.processList, columns, len(m.processFlat), func(row, col int) { cell(m.processFlat[row], col) })
				}).FillWidth().Grow(1).Label("Process table")
				tableRowMenu(tbl, &m.procMenuHits, func(id int, menu *ui.Menu) {
					m.selectProcessRow(uint32(id))
					m.processMenu(menu, m.processByPID[uint32(id)])
				})
				if tbl.Submitted() {
					m.showDetails = true
				}
			}
		})
	}
	selected, ok := m.selectedProcess()
	if m.showDetails && ok {
		if m.detailSplit <= 0 {
			// The split's size is the top pane's height: two thirds of the
			// room leaves the details dock its third.
			_, h := c.Size()
			m.detailSplit = max(120, (h-41)*2/3)
		}
		m.requestInspection(selected.PID)
		ui.SplitVertical(c, &m.detailSplit, func() {
			table()
		}, func() {
			m.processDetails(c)
		}).Grow(1).MinHeight(0).FillWidth()
	} else {
		ui.Column(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			table()
		})
	}
}

// modeButton is the redesign's plain .btn used as a toggle: border, hover,
// and the selected fill of .btn.on.
func (m *Model) modeButton(c *ui.Context, t *ui.Theme, label string, on bool, choose func()) {
	e := ui.Row(c).Height(28).PaddingX(12).AlignItems(ui.Center).Radius(t.Radius).Label(label)
	e.BorderWidth(1).BorderColor(t.Border)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if on {
			p.Fill(r, t.Accent, t.Radius)
		} else if e.Hovered() {
			p.Fill(r, t.SurfaceHover, t.Radius)
		}
	})
	e.Children(func() {
		tx := ui.Text(c, label).SingleLine()
		if on {
			tx.TextColor(t.AccentText).Bold()
		} else {
			tx.TextColor(t.TextMuted)
		}
	})
	if e.Clicked() && !on {
		choose()
	}
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

// loadIcons resolves icons on this goroutine and applies them to the cache in
// batches: a post per icon would queue a frame request each, and the storm of
// frames delays input handling (e.g. the first right-click's menu frame).
func (m *Model) loadIcons() {
	pending := make(map[string]*ui.Bitmap)
	var flush <-chan time.Time
	var timer *time.Timer
	apply := func() {
		if len(pending) == 0 {
			return
		}
		batch := pending
		pending = make(map[string]*ui.Bitmap)
		m.post(func() {
			for path, icon := range batch {
				delete(m.iconPending, path)
				if icon == nil {
					icon = m.iconCache[""]
				}
				if len(m.iconCache) < 256 {
					m.iconCache[path] = icon
				}
			}
		})
	}
	for {
		select {
		case path := <-m.iconRequests:
			pending[path] = systemIcon(path)
			if timer == nil {
				timer = time.NewTimer(300 * time.Millisecond)
				flush = timer.C
			}
		case <-flush:
			apply()
			timer = nil
			flush = nil
		case <-m.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
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
					p.Fill(r, t.Accent, 5)
				} else if e.Hovered() {
					p.Fill(r, t.SurfaceHover, 5)
				}
			})
			e.Children(func() {
				tx := ui.Text(c, name).SingleLine()
				if on {
					tx.TextColor(t.AccentText).Bold()
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
			p.Fill(r, t.Accent, 12)
		} else if e.Hovered() {
			p.Fill(r, t.SurfaceHover, 12)
		}
	})
	e.Children(func() {
		tx := ui.Text(c, name).SingleLine()
		if on {
			tx.TextColor(t.AccentText).Bold()
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
			ui.Text(c, fmt.Sprintf("%s modules · %s threads", format.Int(uint64(len(m.inspectedModules))), format.Int(uint64(len(m.inspectedThreads))))).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, m.inspectError).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted)
			if m.iconButton(c, "x", "Close details").Clicked() {
				m.showDetails = false
			}
		})
		ui.Box(c).Height(6).Shrink(0)
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
			tableChrome(c, func() ui.Element {
				return ui.Table(c, &m.detailList, cols, len(m.inspectedModules), func(i, col int) {
					r := m.inspectedModules[i]
					s := r.Name
					if col == 1 {
						s = format.Bytes(r.Size)
					} else if col == 2 {
						s = r.Path
					}
					ui.Text(c, s).SingleLine().Tooltip(r.Path)
				})
			}).Grow(1).FillWidth()
		} else if m.detailTab == "Threads" {
			cols := []ui.TableColumn{{Title: "Thread ID", Width: 140}, {Title: "Owner PID", Width: 140}, {Title: "Base priority"}}
			tableChrome(c, func() ui.Element {
				return ui.Table(c, &m.detailList, cols, len(m.inspectedThreads), func(i, col int) {
					r := m.inspectedThreads[i]
					s := fmt.Sprint(r.ID)
					if col == 1 {
						s = fmt.Sprint(r.OwnerPID)
					} else if col == 2 {
						s = fmt.Sprint(r.Priority)
					}
					ui.Text(c, s)
				})
			}).Grow(1).FillWidth()
		} else if m.detailTab == "Handles" {
			ui.Row(c).Height(30).Shrink(0).PaddingX(10).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.SearchField(c, &m.handleQuery).Placeholder("Filter type or name").Width(220)
				if ui.Button(c, "Search handles").Clicked() {
					m.requestHandleSearch(m.inspectedPID)
				}
				ui.Text(c, fmt.Sprintf("%s results", format.Int(uint64(len(m.handles))))).TextColor(c.Theme().TextMuted)
			})
			cols := []ui.TableColumn{{Title: "Handle", Width: 90}, {Title: "Type", Width: 130}, {Title: "Name"}, {Title: "Access", Width: 90, Align: ui.End}}
			tableChrome(c, func() ui.Element {
				return ui.Table(c, &m.detailList, cols, len(m.handles), func(i, col int) {
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
				})
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
			ui.Row(c).Height(30).Shrink(0).PaddingX(10).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("Thread %d · %d frames", threadID, len(frames)))
				if ui.Button(c, "Refresh stack").Clicked() {
					m.requestStack(m.inspectedPID, threadID)
				}
			})
			cols := []ui.TableColumn{{Title: "Address", Width: 150}, {Title: "Symbol"}, {Title: "Offset", Width: 100, Align: ui.End}}
			tableChrome(c, func() ui.Element {
				return ui.Table(c, &m.detailList, cols, len(frames), func(i, col int) {
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
				})
			}).Grow(1).FillWidth()
			if m.stackErr != "" {
				ui.Text(c, m.stackErr).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
			}
		}
	})
}
