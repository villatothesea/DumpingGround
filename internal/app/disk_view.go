package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
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
	m.stopWatch()
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
	m.mapMotion = mapMotion{}
	m.mapAnimating = false
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
			m.startWatch()
			if m.diskLive {
				m.diskStatus += " · live"
			}
		}
	default:
	}
}

// startWatch begins tailing the volume's USN change journal after a scan.
// Events replay into the snapshot on the UI goroutine, so live filesystem
// changes update the index without rescanning.
func (m *Model) startWatch() {
	m.stopWatch()
	s := m.diskSnapshot
	if s == nil {
		return
	}
	w, err := s.WatchJournal()
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.diskWatcher = w
	m.diskWatchCancel = cancel
	m.diskEvents = make(chan []disk.USNEvent, 4)
	m.diskWatchErr = make(chan error, 1)
	m.diskLive = true
	go func() {
		defer w.Close()
		for {
			if ctx.Err() != nil {
				return
			}
			ev, err := w.Next(ctx)
			if err != nil {
				if ctx.Err() == nil {
					m.diskWatchErr <- err
				}
				return
			}
			select {
			case m.diskEvents <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
}
func (m *Model) stopWatch() {
	if m.diskWatchCancel != nil {
		m.diskWatchCancel()
		m.diskWatchCancel = nil
	}
	m.diskWatcher = nil
	m.diskLive = false
}
func (m *Model) drainWatch() {
	s := m.diskSnapshot
	if s == nil {
		return
	}
	changed := false
	for {
		select {
		case ev := <-m.diskEvents:
			if s.ApplyUSN(ev, nil) {
				changed = true
			}
		case err := <-m.diskWatchErr:
			m.diskStatus = "Live index lost: " + err.Error()
			m.stopWatch()
		default:
			if changed {
				m.diskViewKey = ""
				m.mapRoot = -1
			}
			return
		}
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
	m.stopWatch()
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
	m.mapMotion = mapMotion{}
	m.mapAnimating = false
	m.diskStatus = "Index cleared"
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
	ui.Row(c).FillWidth().Height(44).Shrink(0).PaddingX(12).Gap(8).AlignItems(ui.Center).Children(func() {
		for _, volume := range m.diskVolumes {
			m.driveChip(c, t, volume, scanning)
		}
		ui.TextInput(c, &m.diskPath).Label("Scan path").Placeholder("Drive or folder path").Grow(1).MinWidth(120).Disabled(scanning)
		if ui.Button(c, "Browse…").Disabled(scanning).Clicked() {
			m.browse()
		}
		if scanning {
			if ui.Button(c, "Cancel").Disabled(m.diskCancelling).TextColor(t.Danger).Clicked() {
				m.cancelScan()
			}
		} else if ui.PrimaryButton(c, "Scan").Disabled(strings.TrimSpace(m.diskPath) == "").Clicked() {
			m.startScan()
		}
	})
	if scanning {
		m.scanningCard(c, t)
		return
	}
	if m.diskSnapshot == nil {
		m.idleCard(c, t)
		return
	}
	m.resultsView(c, t, m.diskSnapshot)
}

// driveChip is a scan-bar volume shortcut: letter, usage bar and free space.
func (m *Model) driveChip(c *ui.Context, t *ui.Theme, v disk.Volume, scanning bool) {
	on := m.diskDrive == v.Path && m.diskPath == v.Path
	e := ui.Row(c).Height(28).PaddingX(10).Gap(8).AlignItems(ui.Center).Radius(6).Shrink(0).Background(t.Surface)
	e.BorderWidth(1).BorderColor(t.Border)
	if on {
		e.BorderColor(t.Accent)
	}
	e.Label("Drive " + v.Path).Tooltip(fmt.Sprintf("%s · %s used · %s free · %s total", v.Label, format.Bytes(v.Total-v.Free), format.Bytes(v.Free), format.Bytes(v.Total)))
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if on {
			p.Fill(r, t.Accent.Alpha(0.15), 6)
		} else if e.Hovered() && !scanning {
			p.Fill(r, t.SurfaceHover, 6)
		}
	})
	e.Children(func() {
		ui.Text(c, v.Path).Bold().SingleLine()
		used := 0.0
		if v.Total > 0 {
			used = float64(v.Total-v.Free) / float64(v.Total)
		}
		ui.Progress(c, used).Width(34).Height(4)
		ui.Text(c, format.Bytes(v.Free)+" free").FontSize(11).TextColor(t.TextMuted).SingleLine()
	})
	if !scanning && e.Clicked() {
		m.diskDrive = v.Path
		m.diskPath = v.Path
	}
}

// idleCard is the centered placeholder the page shows before the first scan.
func (m *Model) idleCard(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		card := ui.Column(c).Width(460).Padding(16, 20).Gap(10).Background(t.Surface).Radius(8)
		card.BorderWidth(1).BorderColor(t.Border)
		card.Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				icon(c, "hdd", 26).TextColor(t.TextMuted)
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					title, note := "Custom folder", m.diskPath
					for _, v := range m.diskVolumes {
						if v.Path == m.diskPath {
							title, note = v.Path+"  "+v.Label, "NTFS · whole-drive scans try the read-only MFT path"
						}
					}
					ui.Text(c, title).FontSize(14).Bold().SingleLine()
					ui.Text(c, note).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
				})
			})
			for _, v := range m.diskVolumes {
				if v.Path == m.diskPath {
					ui.Row(c).Gap(28).Children(func() {
						m.stat(c, t, "Used", format.Bytes(v.Total-v.Free))
						m.stat(c, t, "Free", format.Bytes(v.Free))
						m.stat(c, t, "Total", format.Bytes(v.Total))
					})
					ui.Progress(c, float64(v.Total-v.Free)/float64(max(v.Total, 1))).FillWidth().Height(6)
				}
			}
		})
		ui.Text(c, "Pick a drive or browse for a folder, then Scan (F5). Scans run in the background and can be cancelled.").PaddingY(12).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
	})
}

func (m *Model) stat(c *ui.Context, t *ui.Theme, name, value string) {
	ui.Column(c).Children(func() {
		ui.Text(c, name).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(16).Bold()
	})
}

// scanningCard mirrors the prototype's centered scan card with live counters.
func (m *Model) scanningCard(c *ui.Context, t *ui.Theme) {
	p := m.diskProgress
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		card := ui.Column(c).Width(460).Padding(16, 20).Gap(10).Background(t.Surface).Radius(8)
		card.BorderWidth(1).BorderColor(t.Border)
		card.Children(func() {
			title := "Scanning"
			if p.Method != "" {
				title += " · " + p.Method
			}
			if m.diskCancelling {
				title = "Cancelling"
			}
			ui.Text(c, title).FontSize(14).Bold()
			ui.Row(c).Gap(28).FillWidth().Children(func() {
				m.stat(c, t, "Files", fmt.Sprintf("%d files", p.Files))
				m.stat(c, t, "Folders", fmt.Sprint(p.Dirs))
				m.stat(c, t, "Tallied", format.Bytes(p.Allocated))
				m.stat(c, t, "Elapsed", fmt.Sprintf("%.1f s", time.Since(m.diskStarted).Seconds()))
				m.stat(c, t, "Skipped", fmt.Sprint(p.Skipped))
			})
			ui.Text(c, p.Current).SingleLine().Ellipsis("…").Tooltip(p.Current).TextColor(t.TextMuted)
			ui.Progress(c, -1).FillWidth().Height(4)
		})
		ui.Text(c, "Cancel keeps the partial results collected so far.").PaddingY(12).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
	})
}

// resultsView is the disk workspace once a snapshot exists: view bar with
// breadcrumbs, the split of table and types, the map and the selection bar.
func (m *Model) resultsView(c *ui.Context, t *ui.Theme, s *disk.Snapshot) {
	if s.Fallback != "" {
		ui.Text(c, s.Method+" · "+s.Fallback).FillWidth().SingleLine().FontSize(11).Padding(3, 12).TextColor(t.TextMuted).Tooltip(s.Fallback)
	}
	bar := ui.Row(c).Height(40).FillWidth().Shrink(0).PaddingX(12).Gap(6).AlignItems(ui.Center)
	bar.BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	bar.Children(func() {
		up := m.iconButton(c, "up", "Up one level")
		up.Label("Up").Disabled(m.diskRoot == s.Root)
		if up.Clicked() {
			m.navigateDisk(s.Nodes[m.diskRoot].Parent)
		}
		m.breadcrumbs(c, t, s)
		ui.Spacer(c)
		root := s.Nodes[m.diskRoot]
		ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, fmt.Sprintf("Allocated %s · Logical %s · %d files ·", format.Bytes(root.Allocated), format.Bytes(root.Bytes), s.Files)).FontSize(11).TextColor(t.TextMuted).SingleLine()
			skip := ui.Row(c).Gap(3).Padding(1, 5).Radius(4).AlignItems(ui.Center).Label("What is skipped").Tooltip("Entries the scan could not index — click for details")
			skip.Draw(func(p *ui.Painter, r ui.Rect) {
				if skip.Hovered() {
					p.Fill(r, t.SurfaceHover, 4)
				}
			})
			skip.Children(func() {
				ui.Text(c, fmt.Sprintf("%d skipped", s.Skipped)).FontSize(11).TextColor(t.TextMuted).SingleLine()
				icon(c, "info", 11).TextColor(t.Accent)
			})
			if skip.Clicked() {
				m.diskInfoOpen = true
			}
			ui.Text(c, fmt.Sprintf("· %d estimates", s.Estimated)).FontSize(11).TextColor(t.TextMuted).SingleLine().Tooltip("estimates: files whose on-disk size had to be guessed, e.g. no data attribute read")
		})
		if m.extensionFilter != "" {
			m.extChip(c, t)
		}
		flat := m.diskMode == "Files" || m.diskQuery != "" || m.extensionFilter != ""
		m.segPair(c, t, "Disk view", []string{"Tree", "Flat"}, b2i(flat), func(i int) {
			if i == 1 {
				m.diskMode = "Files"
			} else {
				m.diskMode = "Tree"
				m.diskQuery = ""
				m.extensionFilter = ""
				m.extensionSelected = -1
			}
			m.diskViewKey = ""
			m.diskSelected = -1
		})
		if m.iconTextButton(c, "export", "Export CSV").Clicked() {
			m.exportDisk()
		}
		m.iconButton(c, "more", "More").Menu(func(menu *ui.Menu) {
			if menu.Item("Clear index").Chosen() {
				m.clearDisk()
			}
			menu.Separator()
			if menu.Item("Animate transitions").Checked(m.animateMap).Chosen() {
				m.animateMap = !m.animateMap
			}
		})
	})
	m.prepareDiskRows()
	m.prepareMap()
	w, h := c.Size()
	if m.diskLeft == 0 {
		m.diskLeft = w * 0.70
	}
	m.diskLeft = min(m.diskLeft, w-210)
	m.diskSplit = min(m.diskSplit, max(120, (h-270)*0.60))
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
		ui.SplitVertical(c, &m.diskSplit, func() {
			ui.Split(c, &m.diskLeft, func() { m.diskTable(c) }, func() { m.extensionTable(c) }).Fill()
		}, func() { m.diskMap(c) }).Grow(1).MinHeight(0).FillWidth()
	})
}

// breadcrumbs renders the ancestor chain of the current directory.
func (m *Model) breadcrumbs(c *ui.Context, t *ui.Theme, s *disk.Snapshot) {
	var chain []int
	for id := m.diskRoot; id >= 0 && len(chain) <= len(s.Nodes); id = s.Nodes[id].Parent {
		chain = append([]int{id}, chain...)
		if id == s.Root {
			break
		}
	}
	ui.Row(c).Gap(2).MinWidth(0).AlignItems(ui.Center).Children(func() {
		for i, id := range chain {
			name := s.Nodes[id].Name
			if id == s.Root {
				name = s.FullPath(id)
			}
			last := i == len(chain)-1
			m.crumb(c, t, name, id, last)
			if !last {
				icon(c, "right", 12).TextColor(t.TextMuted)
			}
		}
	})
}

// crumb is one breadcrumb segment: subtle text button, bold and inert when last.
func (m *Model) crumb(c *ui.Context, t *ui.Theme, label string, id int, last bool) {
	e := ui.Row(c).Padding(2, 6).Radius(4).MaxWidth(200)
	if !last {
		e.Draw(func(p *ui.Painter, r ui.Rect) {
			if e.Hovered() {
				p.Fill(r, t.SurfaceHover, 4)
			}
		})
	}
	e.Children(func() {
		text := ui.Text(c, label).SingleLine().Ellipsis("…")
		if last {
			text.Bold()
		}
	})
	if !last && e.Clicked() {
		m.navigateDisk(id)
	}
}

// extChip is the active type filter shown as a removable chip.
func (m *Model) extChip(c *ui.Context, t *ui.Theme) {
	chip := ui.Row(c).Height(24).PaddingX(8).Gap(4).AlignItems(ui.Center).Radius(12).Shrink(0)
	chip.BorderWidth(1).BorderColor(t.Accent)
	chip.Draw(func(p *ui.Painter, r ui.Rect) { p.Fill(r, t.Accent.Alpha(0.15), 12) })
	chip.Children(func() {
		ui.Box(c).Size(8, 8).Radius(2).Background(extensionColor(m.extensionFilter))
		ui.Text(c, m.extensionFilter).FontSize(11).SingleLine()
		x := m.iconButton(c, "x", "Clear type filter").Size(16, 16)
		if x.Clicked() {
			m.extensionFilter = ""
			m.extensionSelected = -1
			m.diskViewKey = ""
			m.diskSelected = -1
		}
	})
}

func (m *Model) navigateDisk(id int) {
	if m.diskSnapshot == nil || id < 0 || id >= len(m.diskSnapshot.Nodes) || !m.diskSnapshot.Nodes[id].Dir {
		return
	}
	m.diskRoot = id
	m.diskMode = "Tree"
	m.diskQuery = ""
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
	if s == nil || m.diskRoot < 0 || m.diskRoot >= len(s.Nodes) {
		return
	}
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
	cols := []ui.TableColumn{{ID: "name", Title: "Name", Width: 280, Sortable: true}, {ID: "logical", Title: "Logical size", Width: 110, Align: ui.End, Sortable: true}, {ID: "allocated", Title: "Allocated", Width: 110, Align: ui.End, Sortable: true}, {Title: "% of folder", Width: 110, Align: ui.End}, {Title: "Path / status"}}
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
				pct := 100 * float64(m.diskValue(n)) / float64(den)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Progress(c, pct/100).Width(42).Height(4)
					ui.Text(c, format.Percent(pct)).SingleLine()
				}).Tooltip(s.FullPath(id)).ContextMenu(func(menu *ui.Menu) { m.diskMenu(menu, id) })
				return
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
		submitted = tableChrome(c, func() ui.Element {
			return ui.OutlineTable(c, &m.diskTree, cols, m.diskRows, func(id int) []int { return s.Nodes[id].Children }, cell)
		}).Fill().Label("Disk table").Submitted()
		if m.diskSelected >= 0 && m.diskSelected < m.diskTree.Rows() {
			selectedID = m.diskTree.Item(m.diskSelected)
		}
	} else {
		m.diskFiles.Selected = &m.diskSelected
		m.diskFiles.Sort = &m.diskSort
		m.diskFiles.Key = func(i int) any { return m.diskRows[i] }
		submitted = tableChrome(c, func() ui.Element {
			return ui.Table(c, &m.diskFiles, cols, len(m.diskRows), func(i, col int) { cell(m.diskRows[i], col) })
		}).Fill().Label("Disk table").Submitted()
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
	s := m.diskSnapshot
	if s == nil || m.diskRoot < 0 || m.diskRoot >= len(s.Nodes) {
		return
	}
	if m.mapRoot == m.diskRoot && m.mapMetric == m.diskMetric {
		return
	}
	m.mapRoot = m.diskRoot
	m.mapMetric = m.diskMetric
	m.mapWidth = 0
	m.mapHeight = 0
	m.diskRects = nil
	m.diskItems = nil
	m.diskExtensions = nil
	// The map is intentionally one hierarchy level deep: a directory is one
	// rectangle whose weight is its aggregate size. Entering it reveals its children.
	m.mapLogMin, m.mapLogMax = 0, 0
	for _, id := range s.Nodes[m.diskRoot].Children {
		if v := m.diskValue(s.Nodes[id]); v > 0 {
			m.diskItems = append(m.diskItems, treemap.Item{ID: id, Value: v})
		}
	}
	for i, it := range m.diskItems {
		lv := math.Log(float64(it.Value) + 1)
		if i == 0 || lv < m.mapLogMin {
			m.mapLogMin = lv
		}
		if i == 0 || lv > m.mapLogMax {
			m.mapLogMax = lv
		}
	}
	stats := map[string]extensionStat{}
	stack := []int{m.diskRoot}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := s.Nodes[id]
		if n.Dir {
			stack = append(stack, n.Children...)
			continue
		}
		ext := fileExtension(n.Name)
		st := stats[ext]
		st.Extension = ext
		st.Files++
		st.Bytes += m.diskValue(n)
		stats[ext] = st
	}
	for _, st := range stats {
		m.diskExtensions = append(m.diskExtensions, st)
	}
	slices.SortFunc(m.diskExtensions, func(a, b extensionStat) int { return cmp.Compare(b.Bytes, a.Bytes) })
}

func (m *Model) diskMap(c *ui.Context) {
	m.prepareMap()
	t := c.Theme()
	animate := m.animateMap && !c.Preferences().ReduceMotion
	ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).FillWidth().Height(36).Shrink(0).PaddingX(12).Gap(8).AlignItems(ui.Center).Children(func() {
			s := m.diskSnapshot
			up := m.iconButton(c, "up", "Up one level")
			up.Label("Up").Disabled(s == nil || m.diskRoot == s.Root)
			if up.Clicked() && s != nil {
				m.navigateDisk(s.Nodes[m.diskRoot].Parent)
			}
			ui.Text(c, "Space map").Bold().Shrink(0)
			if s != nil && m.diskNode >= 0 && m.diskNode < len(s.Nodes) {
				n := s.Nodes[m.diskNode]
				ui.Box(c).Size(8, 8).Radius(2).Background(m.mapColor(m.diskNode)).Shrink(0)
				name := n.Name
				if n.Dir {
					name += "/"
				}
				ui.Text(c, name).Bold().SingleLine().Shrink(0)
				ui.Text(c, format.Bytes(m.diskValue(n))).TextColor(t.TextMuted).Shrink(0)
				path := s.FullPath(m.diskNode)
				ui.Text(c, path).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted).Tooltip(path)
				if m.iconTextButton(c, "folder", "Open location").Clicked() && m.window != nil {
					mygo.Shell.ShowItemInFolder(path)
				}
				if m.iconTextButton(c, "copy", "Copy path").Clicked() && m.window != nil {
					mygo.Clipboard.WriteText(path)
				}
			} else {
				hover := m.mapHover
				if hover == "" {
					hover = "Click a folder to zoom · click a file to locate its row"
				}
				ui.Text(c, hover).SingleLine().Grow(1).MinWidth(0).FontSize(11).TextColor(t.TextMuted).Ellipsis("…")
				icon(c, "info", 14).TextColor(t.TextMuted).Tooltip("One directory level at a time: folders are single blocks you zoom into. Color follows block size: large blocks trend red, small ones green.")
			}
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
				m.diskRects = treemap.Layout(m.diskItems, r.W, r.H)
				sum := 0.0
				for _, it := range m.diskItems {
					sum += float64(it.Value)
				}
				next := mapScene{Root: m.diskRoot, Items: len(m.diskItems), Sum: sum, Width: r.W, Height: r.H, Rects: m.diskRects}
				old := m.mapMotion.Scene
				enter, back := -1, -1
				if old.Root != m.diskRoot {
					enter = m.childBelow(m.diskRoot, old.Root)
					back = m.childBelow(old.Root, m.diskRoot)
				}
				// Only a changed scene animates; a resize of the same scene snaps.
				changed := old.Root != next.Root || old.Items != next.Items || old.Sum != next.Sum
				m.mapMotion.retarget(next, p.Now(), animate && changed, enter, back)
				m.mapWidth, m.mapHeight = r.W, r.H
			}
			progress := m.mapMotion.progress(p.Now(), animate)
			m.mapAnimating = progress < 1
			v := mapEase(progress)
			if m.mapAnimating {
				p.AnimationFrame()
				for i, b := range m.mapMotion.Previous {
					m.paintMapCell(p, r, interpolateRect(b, m.mapMotion.PreviousTo[i], v), 1-v, false)
				}
			}
			for i, b := range m.diskRects {
				if m.mapAnimating {
					b = interpolateRect(m.mapMotion.From[i], b, v)
				}
				m.paintMapCell(p, r, b, v, true)
			}
		})
	})
}

// childBelow returns the first level below ancestor containing node.
func (m *Model) childBelow(node, ancestor int) int {
	if m.diskSnapshot == nil || node == ancestor {
		return -1
	}
	for steps := 0; steps < len(m.diskSnapshot.Nodes); steps++ {
		if node < 0 || node >= len(m.diskSnapshot.Nodes) {
			break
		}
		p := m.diskSnapshot.Nodes[node].Parent
		if p == ancestor {
			return node
		}
		node = p
	}
	return -1
}
func (m *Model) paintMapCell(p *ui.Painter, viewport ui.Rect, b treemap.Rect, alpha float32, labels bool) {
	if alpha <= 0 || m.diskSnapshot == nil || b.ID < 0 || b.ID >= len(m.diskSnapshot.Nodes) {
		return
	}
	x, y := max(b.X, 0), max(b.Y, 0)
	w, h := min(b.X+b.W-1, viewport.W)-x, min(b.Y+b.H-1, viewport.H)-y
	if w < 1 || h < 1 {
		return
	}
	n := m.diskSnapshot.Nodes[b.ID]
	color := m.mapColor(b.ID)
	label := n.Name
	if n.Dir {
		label += "/"
	}
	box := ui.Rect{X: viewport.X + x, Y: viewport.Y + y, W: w, H: h}
	p.Fill(box, color.Alpha(alpha), 2)
	if b.ID == m.diskNode {
		p.Stroke(box, ui.RGB(255, 255, 255).Alpha(alpha), 2, 2)
	}
	if labels && w > 60 && h > 24 {
		// The muted tiles sit around mid luminance: light tiles take dark
		// text, dark ones white.
		ink := ui.RGB(255, 255, 255)
		if float32(color.R)*0.2126+float32(color.G)*0.7152+float32(color.B)*0.0722 > 140 {
			ink = ui.RGB(43, 43, 48)
		}
		runes := []rune(label)
		maxChars := int((w - 10) / 8)
		if len(runes) > maxChars && maxChars > 1 {
			label = string(runes[:maxChars-1]) + "…"
		}
		p.Clip(box, 0, func() {
			p.Text(box.X+5, box.Y+5, label, 12, ink.Alpha(alpha))
			if h > 46 {
				p.Text(box.X+5, box.Y+24, format.Bytes(m.diskValue(n)), 11, ink.Alpha(alpha*.85))
			}
		})
	}
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
	table := tableChrome(c, func() ui.Element {
		return ui.Table(c, &m.extensionList, cols, len(m.diskExtensions), func(i, col int) {
			s := m.diskExtensions[i]
			if col == 0 {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(8, 8).Radius(2).Background(extensionColor(s.Extension))
					ui.Text(c, s.Extension).SingleLine()
				})
				return
			}
			text := fmt.Sprint(s.Files)
			if col == 2 {
				text = format.Bytes(s.Bytes)
			}
			ui.Text(c, text).SingleLine()
		})
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

// mapColor colors a treemap block by its size among the visible blocks:
// the largest sit in dusty reds, the smallest in sage green, with a muted
// rainbow in between (log scale, so moderate differences stay readable).
func (m *Model) mapColor(id int) ui.Color {
	if m.diskSnapshot == nil || id < 0 || id >= len(m.diskSnapshot.Nodes) {
		return ui.Hex("#9a9aa0")
	}
	lv := math.Log(float64(m.diskValue(m.diskSnapshot.Nodes[id])) + 1)
	pos := 0.5
	if m.mapLogMax > m.mapLogMin {
		pos = (lv - m.mapLogMin) / (m.mapLogMax - m.mapLogMin)
	}
	return morandiRamp(pos)
}

// morandiRamp interpolates a muted palette from small (sage green) to
// large (dusty red): the colors a Morandi still life would use, nothing
// saturated.
func morandiRamp(t float64) ui.Color {
	anchors := []ui.Color{
		ui.Hex("#84a58a"), // smallest · sage green
		ui.Hex("#a3ad7c"), // olive
		ui.Hex("#c4ab74"), // mustard
		ui.Hex("#c08d6b"), // terracotta
		ui.Hex("#b4736e"), // dusty red
		ui.Hex("#a26570"), // largest · deep muted red
	}
	t = max(0, min(1, t))
	pos := t * float64(len(anchors)-1)
	i := min(int(pos), len(anchors)-2)
	f := float32(pos - float64(i))
	a, b := anchors[i], anchors[i+1]
	return ui.RGB(
		uint8(float32(a.R)+(float32(b.R)-float32(a.R))*f),
		uint8(float32(a.G)+(float32(b.G)-float32(a.G))*f),
		uint8(float32(a.B)+(float32(b.B)-float32(a.B))*f),
	)
}
