package app

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/dumpingground/dumpingground/internal/process"
	"github.com/dumpingground/dumpingground/internal/treemap"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// appIcon is the rendered product mark (logo option 10) for the window
// and taskbar; regenerate with DG_ICON_OUT=... go test -run TestRenderAppIcon.
//
//go:embed appicon.png
var appIcon []byte

type page uint8

const (
	processPage page = iota
	diskPage
	uninstallPage
	startupPage
)

type processResult struct {
	rows []process.Info
	err  error
}
type scanResult struct {
	snapshot *disk.Snapshot
	err      error
}
type inspectionResult struct {
	pid                 uint32
	modules             []process.Module
	threads             []process.Thread
	err                 error
	info                process.Info
	handles             []process.Handle
	stacks              map[uint32][]process.StackFrame
	handleErr, stackErr error
}

type Model struct {
	window                          *mygo.Window
	processSvc                      *process.Service
	processRows                     []process.Info
	processByPID                    map[uint32]process.Info
	processAgg                      map[uint32]process.Info
	processChildren                 map[uint32][]uint32
	processRoots                    []uint32
	processTree                     ui.OutlineState[uint32]
	processList                     ui.ListState
	processSelected                 int
	processQuery                    string
	processSort                     ui.SortOrder
	processFlat                     []uint32
	processTreeMode                 bool
	processPaused                   bool
	minimized                       bool
	processInterval                 string
	processUpdates                  chan processResult
	refresh                         chan struct{}
	samplerActive                   atomic.Bool
	sampleInterval                  atomic.Int64
	ctx                             context.Context
	close                           context.CancelFunc
	page                            page
	lastProcessError                string
	inspectedPID                    uint32
	inspectedModules                []process.Module
	inspectedThreads                []process.Thread
	inspection                      chan inspectionResult
	inspectError                    string
	inspectedInfo                   process.Info
	handles                         []process.Handle
	handleQuery                     string
	handleLoading                   bool
	handleErr                       string
	stacks                          map[uint32][]process.StackFrame
	stackLoading                    bool
	stackErr                        string
	stackThread                     uint32
	detailTab                       string
	detailList                      ui.ListState
	showDetails                     bool
	terminateOpen, terminateTree    bool
	terminateTarget                 process.Info
	lastQuery                       string
	lastSort                        ui.SortOrder
	dirtyProcesses                  bool
	iconCache                       map[string]*ui.Bitmap
	iconPending                     map[string]bool
	iconRequests                    chan string
	detailSplit                     float32
	themeMode                       int
	settingsOpen                    bool
	diskInfoOpen                    bool
	reservedQuery                   string
	sys                             systemUsage
	status                          string
	diskScanner                     disk.Scanner
	diskPath, diskDrive             string
	diskVolumes                     []disk.Volume
	diskSnapshot                    *disk.Snapshot
	diskRoot, diskSelected          int
	diskTree                        ui.OutlineState[int]
	diskFiles                       ui.ListState
	diskRows                        []int
	diskSort                        ui.SortOrder
	diskQuery, diskMode, diskMetric string
	diskViewKey                     string
	diskProgress                    disk.Progress
	diskStatus                      string
	diskCancel                      context.CancelFunc
	diskCancelling                  bool
	diskProgressCh                  chan disk.Progress
	diskDone                        chan scanResult
	diskStarted                     time.Time
	diskItems                       []treemap.Item
	diskRects                       []treemap.Rect
	mapRoot                         int
	mapMetric                       string
	mapWidth, mapHeight             float32
	mapHover                        string
	mapMotion                       mapMotion
	mapLogMin, mapLogMax            float64
	animateMap                      bool
	mapAnimating                    bool
	diskSplit                       float32
	diskLeft                        float32
	diskExtensions                  []extensionStat
	extensionList                   ui.ListState
	extensionSelected               int
	extensionFilter                 string
	diskNode                        int
	errorList                       ui.ListState
	diskWatcher                     *disk.Watcher
	diskWatchCancel                 context.CancelFunc
	diskEvents                      chan []disk.USNEvent
	diskWatchErr                    chan error
	diskLive                        bool
	events                          chan func()
	// CaptureDir writes only this application's frames during explicit diagnostics.
	CaptureDir  string
	InitialPage string
	InitialScan string
	AutoClose   time.Duration
}

func New() *Model {
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{processSvc: process.NewService(), processByPID: map[uint32]process.Info{}, processSelected: -1,
		processSort: ui.SortOrder{Column: "name"}, processInterval: "1 s", ctx: ctx, close: cancel,
		processUpdates: make(chan processResult, 1), refresh: make(chan struct{}, 1), inspection: make(chan inspectionResult, 1), events: make(chan func(), 8),
		iconCache:   make(map[string]*ui.Bitmap),
		iconPending: make(map[string]bool), iconRequests: make(chan string, 32),
		diskStatus: "Choose a drive or folder to scan", diskMode: "Tree", diskMetric: "Allocated", diskSelected: -1, diskNode: -1, extensionSelected: -1, mapRoot: -1, diskSplit: 300, animateMap: true, detailTab: "Properties"}
}
func (m *Model) Run() error {
	if os.Getenv("DG_DEBUG_INSTANCE") != "" {
		mygo.App.SetName("DumpingGroundDebug")
	} else {
		mygo.App.SetName("DumpingGround")
		if !mygo.App.RequestSingleInstanceLock() {
			m.close()
			return nil
		}
	}
	mygo.App.WhenReady(func() {
		if m.InitialPage == "disk" {
			m.page = diskPage
		}
		m.iconCache[""] = systemIcon("")
		go m.loadIcons()
		m.window = mygo.NewWindow(mygo.WindowOptions{Title: "DumpingGround", Width: 1180, Height: 760, MinWidth: 880, MinHeight: 580, TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 40, Content: ui.View(m.view)})
		if err := m.window.SetIcon(appIcon); err != nil {
			log.Print("window icon:", err)
		}
		mygo.App.OnSecondInstance(func(_ []string, _ string) { m.window.Restore(); m.window.Show(); m.window.Focus() })
		m.window.OnMinimize(func() { m.minimized = true; m.samplerActive.Store(false) })
		m.window.OnRestore(func() {
			m.minimized = false
			m.samplerActive.Store(!m.processPaused && m.page == processPage)
			m.window.Invalidate()
		})
		m.window.OnClosed(func() {
			m.close()
			if m.diskCancel != nil {
				m.diskCancel()
			}
		})
		m.samplerActive.Store(true)
		m.sampleInterval.Store(int64(time.Second))
		go m.sampleProcesses()
		go func() {
			vols := disk.Volumes()
			m.post(func() {
				m.diskVolumes = vols
				if len(vols) > 0 {
					m.diskDrive = vols[0].Path
					if m.diskPath == "" {
						m.diskPath = m.diskDrive
					}
				}
			})
		}()
		if m.InitialScan != "" {
			m.page = diskPage
			m.diskPath = m.InitialScan
			m.startScan()
		}
		if m.AutoClose > 0 {
			go func() {
				select {
				case <-time.After(m.AutoClose):
					mygo.App.Quit()
				case <-m.ctx.Done():
				}
			}()
		}
		if m.CaptureDir != "" {
			go m.captureFrames()
		}
	})
	return mygo.App.Run()
}
func (m *Model) post(fn func()) {
	select {
	case m.events <- fn:
		if m.window != nil {
			m.window.Invalidate()
		}
	case <-m.ctx.Done():
	}
}
func (m *Model) sampleProcesses() {
	force := true
	for {
		if force || m.samplerActive.Load() {
			force = false
			rows, err := m.processSvc.Snapshot()
			select {
			case <-m.processUpdates:
			default:
			}
			select {
			case m.processUpdates <- processResult{rows, err}:
				if m.window != nil {
					m.window.Invalidate()
				}
			case <-m.ctx.Done():
				return
			}
		}
		d := time.Duration(m.sampleInterval.Load())
		if d <= 0 {
			d = time.Second
		}
		timer := time.NewTimer(d)
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-m.refresh:
			force = true
			timer.Stop()
		case <-timer.C:
		}
	}
}
func (m *Model) refreshOnce() {
	m.samplerActive.Store(true)
	select {
	case m.refresh <- struct{}{}:
	default:
	}
}
func (m *Model) drain() {
	for {
		select {
		case fn := <-m.events:
			fn()
		default:
			goto eventsDone
		}
	}
eventsDone:
	select {
	case r := <-m.processUpdates:
		if r.err != nil {
			m.lastProcessError = r.err.Error()
		} else {
			m.processRows = r.rows
			m.lastProcessError = ""
			m.dirtyProcesses = true
		}
	default:
	}
	select {
	case r := <-m.inspection:
		if r.pid == m.inspectedPID {
			m.inspectedModules, m.inspectedThreads = r.modules, r.threads
			m.inspectedInfo = r.info
			m.handles = r.handles
			m.handleErr = ""
			if r.handleErr != nil {
				m.handleErr = r.handleErr.Error()
			}
			m.stacks = r.stacks
			m.stackErr = ""
			if r.stackErr != nil {
				m.stackErr = r.stackErr.Error()
			}
			m.inspectError = ""
			if r.err != nil {
				m.inspectError = r.err.Error()
			}
		}
	default:
	}
	m.drainScan()
	m.drainWatch()
}
func (m *Model) view(c *ui.Context) {
	m.drain()
	if c.Shortcut(ui.Ctrl, ui.Key1) {
		m.page = processPage
	}
	if c.Shortcut(ui.Ctrl, ui.Key2) {
		m.page = diskPage
	}
	if c.Shortcut(ui.Ctrl, ui.Key3) {
		m.page = uninstallPage
	}
	if c.Shortcut(ui.Ctrl, ui.Key4) {
		m.page = startupPage
	}
	if c.Shortcut(0, ui.KeyF5) {
		if m.page == processPage {
			m.refreshOnce()
		} else if m.page == diskPage {
			m.startScan()
		}
	}
	if m.page == diskPage && m.diskSnapshot != nil && m.diskRoot != m.diskSnapshot.Root {
		if c.Shortcut(0, ui.KeyBackspace) || c.Shortcut(0, ui.KeyBack) {
			m.navigateDisk(m.diskSnapshot.Nodes[m.diskRoot].Parent)
		}
	}
	m.samplerActive.Store(!m.processPaused && m.page == processPage && !m.minimized)
	if m.diskCancel != nil {
		c.After(100 * time.Millisecond)
	}
	if m.dirtyProcesses || m.lastQuery != m.processQuery || m.lastSort != m.processSort {
		m.rebuildProcessTree()
		m.dirtyProcesses = false
		m.lastQuery = m.processQuery
		m.lastSort = m.processSort
	}
	if m.themeMode != 0 {
		t := ui.LightTheme()
		if m.themeMode == 2 {
			t = ui.DarkTheme()
		}
		c.SetTheme(t)
	}
	m.sys.sample()
	t := c.Theme()
	ui.Column(c).Fill().AlignItems(ui.Stretch).Font("Segoe UI").FontSize(12).Background(t.Background).Children(func() {
		m.topBar(c, t)
		switch m.page {
		case processPage:
			m.processView(c)
		case diskPage:
			m.diskView(c)
		case uninstallPage:
			m.reservedView(c, "pkg", "Uninstall", "Lists installed software with search, size sorting and removal. Tab position, shortcut and the shared search box are reserved.")
		case startupPage:
			m.reservedView(c, "zap", "Startup items", "Manages programs that start with Windows, with enable and disable controls. Tab position, shortcut and the shared search box are reserved.")
		}
		m.statusBar(c, t)
	})
	if m.settingsOpen {
		m.settingsView(c)
	}
	if m.diskInfoOpen {
		m.diskInfoView(c)
	}
	if m.terminateOpen {
		choice := ui.AlertDialog(c, &m.terminateOpen, "End process?", fmt.Sprintf("%s (PID %d). Unsaved work may be lost.", m.terminateTarget.Name, m.terminateTarget.PID), "Cancel", "End process")
		if choice == 1 {
			target, tree := m.terminateTarget, m.terminateTree
			go func() {
				err := m.processSvc.TerminateInfo(target, tree)
				m.post(func() {
					if err != nil {
						m.status = err.Error()
					} else {
						m.status = "Process ended"
					}
					m.refreshOnce()
				})
			}()
		}
	}
}

// topBar is the 40 DIP strip of the redesign: brand, underline tabs, the
// selection actions, the page-scoped search field and window-level tools.
// The window's title bar is hidden, so the strip doubles as the drag
// region and leaves the room c.TitleBar reports to the system buttons.
func (m *Model) topBar(c *ui.Context, t *ui.Theme) {
	// Keep the controls clear of the window buttons. A zero TitleBar means
	// the caption failed to appear: still leave a corner's room or the
	// window's rounded corner clips the last button.
	right := max(c.TitleBar().Right+4, 48)
	bar := ui.Row(c).FillWidth().Height(40).Shrink(0).Padding(0, right, 0, 12).Gap(4).AlignItems(ui.Center).Background(t.Surface).DragWindow()
	bar.Draw(func(p *ui.Painter, r ui.Rect) { p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, t.Border, 0) })
	bar.Children(func() {
		ui.Row(c).Width(168).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
			logo := ui.Box(c).Size(22, 22).Radius(6).Background(t.Accent).Label("DumpingGround")
			logo.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Icon(glyphs["logo"], ui.Rect{X: r.X + 3, Y: r.Y + 3, W: 16, H: 16}, t.AccentText)
			})
			ui.Text(c, "DumpingGround").FontSize(14).Bold().SingleLine()
		})
		ui.Row(c).Gap(2).Shrink(0).Children(func() {
			m.pageTab(c, t, processPage, "Processes")
			m.pageTab(c, t, diskPage, "Disk usage")
			m.pageTab(c, t, uninstallPage, "Uninstall")
			m.pageTab(c, t, startupPage, "Startup")
		})
		m.selActs(c, t)
		ui.Spacer(c)
		switch m.page {
		case processPage:
			if ui.SearchField(c, &m.processQuery).Label("Search processes").Placeholder("Name, PID or path").Width(240).Changed() {
				m.dirtyProcesses = true
			}
		case diskPage:
			ui.SearchField(c, &m.diskQuery).Label("Filter files").Placeholder("Filter files").Width(240).Disabled(m.diskSnapshot == nil)
		default:
			ui.SearchField(c, &m.reservedQuery).Label("Search").Placeholder("Not available on this page").Width(240).Disabled(true)
		}
		dark := t.Dark
		th := m.iconButton(c, map[bool]string{true: "sun", false: "moon"}[dark], map[bool]string{true: "Switch to light theme", false: "Switch to dark theme"}[dark])
		if th.Clicked() {
			if dark {
				m.themeMode = 1
			} else {
				m.themeMode = 2
			}
		}
		if m.iconButton(c, "gear", "Settings").Clicked() {
			m.settingsOpen = true
		}
	})
}

// pageTab is one top-bar underline tab. It is a plain clickable row rather
// than ui.Tabs so the page change applies in the same frame as the click.
func (m *Model) pageTab(c *ui.Context, t *ui.Theme, pg page, label string) {
	on := m.page == pg
	e := ui.Row(c).Height(30).PaddingX(10).AlignItems(ui.Center).Radius(4).Label(label)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if on {
			p.Fill(ui.Rect{X: r.X + 8, Y: r.Y + r.H - 2, W: r.W - 16, H: 2}, t.Accent, 1)
		} else if e.Hovered() {
			p.Fill(r, t.SurfaceHover, 4)
		}
	})
	e.Children(func() {
		tx := ui.Text(c, label).SingleLine()
		if on {
			tx.Bold()
		} else {
			tx.TextColor(t.TextMuted)
		}
	})
	if e.Clicked() {
		m.page = pg
	}
}

// selActs mirrors the prototype's #selacts: quick actions for the selected
// process live in the top bar right of the tabs and disappear when nothing
// is selected.
func (m *Model) selActs(c *ui.Context, t *ui.Theme) {
	if m.page != processPage {
		return
	}
	selected, ok := m.selectedProcess()
	if !ok {
		return
	}
	ui.Row(c).Gap(6).PaddingX(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c).Width(1).Height(20).Background(t.Border)
		if m.pactButton(c, "folder", "Open location").Disabled(selected.Path == "").Clicked() {
			mygo.Shell.ShowItemInFolder(selected.Path)
		}
		if m.pactButton(c, "copy", "Copy path").Disabled(selected.Path == "").Clicked() {
			mygo.Clipboard.WriteText(selected.Path)
		}
		exited := selected.Access == "Exited"
		killTip := "End process"
		if exited {
			killTip = "Process already exited"
		}
		kill := m.dangerButton(c, "x", "End process").Disabled(selected.PID <= 4 || exited).Tooltip(killTip)
		if kill.Clicked() {
			m.terminateTarget = selected
			m.terminateTree = false
			m.terminateOpen = true
		}
		m.pactButton(c, "down", "More end options").Disabled(selected.PID <= 4 || exited).Menu(func(menu *ui.Menu) {
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

// pactButton is the prototype's .pact: a 28×28 icon-only toolbar button.
func (m *Model) pactButton(c *ui.Context, name, tip string) ui.Element {
	t := c.Theme()
	e := ui.Row(c).Size(28, 28).Radius(t.Radius).Center().Background(t.Surface).Label(tip).Tooltip(tip)
	e.BorderWidth(1).BorderColor(t.Border)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if e.Pressed() {
			p.Fill(r, t.SurfacePressed, t.Radius)
		} else if e.Hovered() {
			p.Fill(r, t.SurfaceHover, t.Radius)
		}
	})
	e.Children(func() { icon(c, name, 14).TextColor(t.Text) })
	return e
}

// statMeter is the status-bar gauge of the redesign: icon, muted label and
// a value that turns orange at 75% and red at 90%.
func (m *Model) statMeter(c *ui.Context, t *ui.Theme, glyphName, label, value string, pct float64, tip string) {
	e := ui.Row(c).Height(22).PaddingX(6).Gap(5).AlignItems(ui.Center).Radius(4).Shrink(0).Tooltip(tip)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if e.Hovered() {
			p.Fill(r, t.SurfaceHover, 4)
		}
	})
	e.Children(func() {
		icon(c, glyphName, 12).TextColor(t.TextMuted)
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		v := ui.Text(c, value).FontSize(11)
		switch {
		case pct >= 90:
			v.TextColor(t.Danger)
		case pct >= 75:
			v.TextColor(t.Warning)
		default:
			v.TextColor(t.Text)
		}
	})
}

// rateMeter shows a label with down/up throughput figures.
func (m *Model) rateMeter(c *ui.Context, t *ui.Theme, glyphName, label, tip string, down, up uint64) {
	e := ui.Row(c).Height(22).PaddingX(6).Gap(4).AlignItems(ui.Center).Radius(4).Shrink(0).Tooltip(tip)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if e.Hovered() {
			p.Fill(r, t.SurfaceHover, 4)
		}
	})
	e.Children(func() {
		icon(c, glyphName, 12).TextColor(t.TextMuted)
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		ui.Row(c).Gap(1).AlignItems(ui.Center).Children(func() {
			icon(c, "dn", 10).TextColor(t.TextMuted)
			ui.Text(c, format.Rate(down)).FontSize(11)
		})
		ui.Row(c).Gap(1).AlignItems(ui.Center).Children(func() {
			icon(c, "upa", 10).TextColor(t.TextMuted)
			ui.Text(c, format.Rate(up)).FontSize(11)
		})
	})
}

// iconTextButton is the redesign's .btn: 28 DIP control face with a leading
// 14 DIP glyph and hover and press feedback.
func (m *Model) iconTextButton(c *ui.Context, name, label string) ui.Element {
	t := c.Theme()
	e := ui.Row(c).Height(28).PaddingX(12).Gap(6).AlignItems(ui.Center).Radius(t.Radius).Background(t.Surface).Label(label)
	e.BorderWidth(1).BorderColor(t.Border)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if e.Pressed() {
			p.Fill(r, t.SurfacePressed, t.Radius)
		} else if e.Hovered() {
			p.Fill(r, t.SurfaceHover, t.Radius)
		}
	})
	e.Children(func() {
		icon(c, name, 14).TextColor(t.Text)
		ui.Text(c, label).SingleLine()
	})
	return e
}

// dangerButton is the destructive .btn: a solid danger face with a white
// label, lighter on hover and darker on press, faded while disabled.
func (m *Model) dangerButton(c *ui.Context, name, label string) ui.Element {
	t := c.Theme()
	e := ui.Row(c).Height(28).PaddingX(12).Gap(6).AlignItems(ui.Center).Radius(t.Radius).Label(label)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		bg := t.Danger
		switch {
		case e.IsDisabled():
			bg = t.Danger.Alpha(0.45)
		case e.Pressed():
			bg = t.Danger.Mix(ui.RGB(0, 0, 0), 0.2)
		case e.Hovered():
			bg = t.Danger.Mix(ui.RGB(255, 255, 255), 0.12)
		}
		p.Fill(r, bg, t.Radius)
	})
	e.Children(func() {
		icon(c, name, 14).TextColor(ui.RGB(255, 255, 255))
		ui.Text(c, label).SingleLine().TextColor(ui.RGB(255, 255, 255))
	})
	return e
}

// iconButton is the top bar's 34×28 glyph button with hover and press states.
func (m *Model) iconButton(c *ui.Context, name, tip string) ui.Element {
	t := c.Theme()
	e := ui.Row(c).Size(34, 28).Radius(6).Center().Tooltip(tip).Label(tip)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if e.Pressed() {
			p.Fill(r, t.SurfacePressed, 6)
		} else if e.Hovered() {
			p.Fill(r, t.SurfaceHover, 6)
		}
	})
	e.Children(func() { icon(c, name, 16).TextColor(t.Text) })
	return e
}

// statusBar mirrors the page status on the left and key hints on the right.
func (m *Model) statusBar(c *ui.Context, t *ui.Theme) {
	bar := ui.Row(c).FillWidth().Height(26).Shrink(0).PaddingX(12).Gap(12).AlignItems(ui.Center).Background(t.Surface)
	bar.Draw(func(p *ui.Painter, r ui.Rect) { p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, t.Border, 0) })
	bar.Children(func() {
		text := m.status
		switch m.page {
		case diskPage:
			text = m.diskStatus
		case uninstallPage, startupPage:
			text = "Reserved"
		}
		if text == "" {
			text = "Ready"
		}
		st := ui.Text(c, text).SingleLine().MinWidth(0).TextColor(t.TextMuted)
		if m.page == diskPage && m.diskLive {
			st.Tooltip("live: the index updates itself through the NTFS change journal — file changes appear as they happen, without rescanning. Click the ⓘ icon for details.")
		}
		m.statMeter(c, t, "cpu", "CPU", fmt.Sprintf("%.0f%%", m.sys.cpu), m.sys.cpu, fmt.Sprintf("Total CPU usage %.0f%%", m.sys.cpu))
		m.statMeter(c, t, "mem", "MEM", fmt.Sprintf("%d%%", m.sys.memLoad), float64(m.sys.memLoad), fmt.Sprintf("Memory %.1f of %.1f GB used (%d%%)", float64(m.sys.memUsed)/1073741824, float64(m.sys.memTotal)/1073741824, m.sys.memLoad))
		var rd, wr uint64
		for _, r := range m.processRows {
			rd += r.ReadRate
			wr += r.WriteRate
		}
		m.rateMeter(c, t, "hdd", "I/O", fmt.Sprintf("Process disk I/O  read %s  ·  write %s", format.Rate(rd), format.Rate(wr)), rd, wr)
		m.rateMeter(c, t, "net", "NET", fmt.Sprintf("Network  down %s  ·  up %s", format.Rate(m.sys.netInRate), format.Rate(m.sys.netOutRate)), m.sys.netInRate, m.sys.netOutRate)
		ui.Spacer(c)
		hint := "F5 refresh · Ctrl+1–4 pages"
		if m.page == diskPage {
			hint = "F5 scan · Backspace up · Ctrl+1–4 pages"
		}
		ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		if m.page == diskPage {
			info := ui.Row(c).Size(20, 20).Center().Radius(4).Label("About scan terms").Tooltip("What skipped, estimates and live mean — click for details")
			info.Draw(func(p *ui.Painter, r ui.Rect) {
				if info.Hovered() {
					p.Fill(r, t.SurfaceHover, 4)
				}
			})
			info.Children(func() { icon(c, "info", 14).TextColor(t.TextMuted) })
			if info.Clicked() {
				m.diskInfoOpen = true
			}
		}
	})
}

// settingsView is the popover of the redesign, a small modal with the two
// scope-wide preferences plus the map transition toggle.
func (m *Model) settingsView(c *ui.Context) {
	ui.Modal(c, &m.settingsOpen, func() {
		ui.Column(c).Width(360).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Text(c, "Settings").Bold().FontSize(14)
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, "Process refresh")
					ui.Text(c, "Automatic sampling on the Processes page").FontSize(11).TextColor(c.Theme().TextMuted)
				})
				if ui.Select(c, &m.processInterval, []string{"1 s", "2 s", "5 s"}).Width(80).Changed() {
					switch m.processInterval {
					case "2 s":
						m.sampleInterval.Store(2e9)
					case "5 s":
						m.sampleInterval.Store(5e9)
					default:
						m.sampleInterval.Store(1e9)
					}
				}
			})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, "Space metric")
					ui.Text(c, "Used by the space map and percent column").FontSize(11).TextColor(c.Theme().TextMuted)
				})
				ui.Select(c, &m.diskMetric, []string{"Allocated", "Logical"}).Width(100).Label("Size metric")
			})
			ui.Checkbox(c, &m.animateMap, "Animate map drill-down").Tooltip("Respects Windows reduced motion")
		})
	})
}

// diskInfoView explains the numbers the disk view reports: what skipped
// entries are, whether they can be scanned, what estimates are, and what
// the live indicator means.
func (m *Model) diskInfoView(c *ui.Context) {
	ui.Modal(c, &m.diskInfoOpen, func() {
		ui.Column(c).Width(430).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Text(c, "About the scan numbers").Bold().FontSize(14)
			m.diskInfoItem(c, "skipped", "Entries the scan could not index: access-denied folders, reparse points (junctions and links, skipped on purpose so nothing counts twice), cloud placeholders whose data is not on this disk, items deleted mid-scan, and records without a usable name. Skipped entries are simply absent — the totals you see only cover what could be read.")
			m.diskInfoItem(c, "Can they be scanned?", "Access-denied items mostly can: run DumpingGround as administrator and scan again. Cloud placeholders cannot — their content lives in the cloud, not on the disk. Reparse points stay skipped by design; scan their target folder instead.")
			m.diskInfoItem(c, "estimates", "Files that were indexed but whose on-disk size had to be guessed, because no data attribute could be read. Their size is rounded up to a cluster.")
			m.diskInfoItem(c, "live", "The index keeps itself current through the NTFS change journal: new, renamed, resized and deleted files update the numbers as they happen — no rescan needed. Live mode needs administrator rights; if the journal wraps under heavy churn the status line says \"Live index lost\" and a rescan restores it.")
		})
	})
}

// diskInfoItem is one titled paragraph of the scan-terms modal.
func (m *Model) diskInfoItem(c *ui.Context, title, body string) {
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, title).Bold()
		ui.Text(c, body).FontSize(11).TextColor(c.Theme().TextMuted).Width(390)
	})
}

// reservedView is the empty state for tabs whose features are planned.
func (m *Model) reservedView(c *ui.Context, glyphName, title, desc string) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		ui.Box(c).Size(56, 56).Radius(14).Center().Background(t.Surface).Children(func() {
			icon(c, glyphName, 26).TextColor(t.TextMuted)
		}).Draw(func(p *ui.Painter, r ui.Rect) { p.Stroke(r, t.Border, 14, 1) })
		ui.Text(c, title).FontSize(15).Bold().PaddingY(4)
		ui.Row(c).Padding(1, 8).Radius(10).Background(t.Surface).Children(func() {
			ui.Text(c, "Reserved · planned").FontSize(11).TextColor(t.TextMuted)
		}).Draw(func(p *ui.Painter, r ui.Rect) { p.Stroke(r, t.Border, 10, 1) })
		ui.Text(c, desc).Width(420).TextAlign(ui.Center).PaddingY(8).TextColor(t.TextMuted)
		ui.Spacer(c)
	})
}

// tableChrome hides a Table's column separator lines and its header
// divider (both draw in the theme's Border color), then redraws the
// header's bottom line itself at a crisp pixel. The header's resize grip
// still works: hovering it shows the accent line, dragging resizes the
// column and a double click fits it to its cells.
func tableChrome(c *ui.Context, build func() ui.Element) ui.Element {
	orig := c.Theme()
	plain := *orig
	plain.Border = orig.Border.Alpha(0)
	c.SetTheme(&plain)
	e := build()
	c.SetTheme(orig)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + orig.Space(8), W: r.W, H: 1}, orig.Border, 0)
	})
	return e
}

func (m *Model) captureFrames() {
	os.MkdirAll(m.CaptureDir, 0755)
	for i := 0; i < 3; i++ {
		select {
		case <-time.After(2 * time.Second):
		case <-m.ctx.Done():
			return
		}
		if data, err := m.window.CapturePage(); err == nil {
			os.WriteFile(fmt.Sprintf("%s/frame-%d.png", m.CaptureDir, i), data, 0644)
		}
	}
}
