package app

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"slices"
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
	hardwarePage
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
	procMenuHits                    []menuHit
	procTop                         map[int]map[uint32]int
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
	appliedThemeMode                int
	settingsOpen                    bool
	diskInfoOpen                    bool
	reservedQuery                   string
	sys                             systemUsage
	status                          string
	diskScanner                     disk.Scanner
	diskPath, diskDrive             string
	diskVolumes                     []disk.Volume
	volsChecked                     time.Time
	volsChecking                    atomic.Bool
	diskSnapshot                    *disk.Snapshot
	diskRoot, diskSelected          int
	diskTree                        ui.OutlineState[int]
	diskFiles                       ui.ListState
	diskRows                        []int
	diskMenuHits                    []menuHit
	diskSort                        ui.SortOrder
	diskQuery, diskMode, diskMetric string
	memRelease                      string
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
	tabStyle                        string
	mapAnimating                    bool
	diskSplit                       float32
	diskLeft                        float32
	diskExtensions                  []extensionStat
	extPending                      []int
	extStats                        map[string]extensionStat
	extensionList                   ui.ListState
	extensionSelected               int
	extensionFilter                 string
	diskNode                        int
	errorList                       ui.ListState
	diskWatcher                     *disk.Watcher
	diskWatchCancel                 context.CancelFunc
	diskEvents                      chan []disk.USNEvent
	diskPending                     [][]disk.USNEvent
	diskLiveDirty                   bool
	diskLiveFlush                   time.Time
	diskMapFlush                    time.Time
	diskWatchErr                    chan error
	diskLive                        bool
	scanPath                        string // the path the current snapshot was built from
	rustPending                     bool // analyze for Rust artifacts once a running scan settles
	rustBusy                        bool // analysis or deletion in flight
	rustOwnsIndex                   bool // the index was built for a Rust scan and is released after it
	rustReport                      *disk.RustReport
	rustMode                        bool // the disk page shows the artifact report
	rustSel                         int  // the cleanup tier previewing its files, -1 none
	rustConfirm                     int
	rustRowConfirm                  string // target path whose row clean button is armed
	rustSortIdle                    bool   // sort report rows by idle days instead of size
	rustRecycle                     bool   // send deletions to the recycle bin where possible
	rustFilterCat                   int    // category filtering the row list, -1 all
	rustGroupSafe                   bool   // group rows under risk-level headers
	rustCollapsed                   map[int]bool // collapsed risk groups
	rustAnStage                     int    // analysis progress stage (disk.RustStage*)
	rustAnVis                       int    // index nodes visited so far
	rustAnFound                     int    // project targets found so far
	rustPathsCat                    int // category whose path list the modal shows, -1 none
	rustPathsOpen                   bool
	rustPathsList                   ui.ListState
	rustMsg                         string
	rustGone                        map[string]bool
	rustList                        ui.ListState
	junkBusy                        bool // junk measurement or cleaning in flight
	junkReport                      *disk.JunkReport
	junkMode                        bool // the disk page shows the junk report
	junkSel                         int  // preset card previewing its rows, -1 none
	junkConfirm                     int  // preset armed for the second click, -1 none
	junkRowConfirm                  string // path whose row clean button is armed
	junkFilterCat                   int    // category filtering the row list, -1 all
	junkGroupRisk                   bool   // group rows under risk-level headers
	junkGroupCat                    bool   // group rows under category headers — stacks with risk
	junkCatFirst                    bool   // when both groupings are on, category is the outer level
	junkCollapsed                   map[string]bool // collapsed group headers, keyed by group path
	junkAnCat                       int    // category currently being measured
	junkPathsCat                    int    // category whose path list the modal shows, -1 none
	junkPathsOpen                   bool
	junkPathsList                   ui.ListState
	junkMsg                         string
	junkDoneMsg                     string // outcome of the last clean, shown once the re-measure lands
	junkList                        ui.ListState
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
		processSort: ui.SortOrder{Column: "name"}, processInterval: "1 s", processTreeMode: true, ctx: ctx, close: cancel,
		processUpdates: make(chan processResult, 1), refresh: make(chan struct{}, 1), inspection: make(chan inspectionResult, 1), events: make(chan func(), 8),
		iconCache:   make(map[string]*ui.Bitmap),
		iconPending: make(map[string]bool), iconRequests: make(chan string, 32),
		diskStatus: "Choose a drive or folder to scan", diskMode: "Tree", diskMetric: "Allocated", diskSelected: -1, diskNode: -1, extensionSelected: -1, mapRoot: -1, diskSplit: 300, animateMap: true, detailTab: "Properties", memRelease: "Automatic", tabStyle: "Pill",
		rustGone: map[string]bool{}, rustSel: -1, rustConfirm: -1, rustPathsCat: -1,
		rustFilterCat: -1, junkSel: -1, junkConfirm: -1, junkFilterCat: -1, junkPathsCat: -1,
		rustCollapsed: map[int]bool{}, junkCollapsed: map[string]bool{}}
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
		m.window = mygo.NewWindow(mygo.WindowOptions{Title: "DumpingGround", Width: 1180, Height: 760, MinWidth: 960, MinHeight: 580, TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 40, Content: ui.View(m.view)})
		if err := m.window.SetIcon(appIcon); err != nil {
			log.Print("window icon:", err)
		}
		mygo.App.OnSecondInstance(func(_ []string, _ string) { m.window.Restore(); m.window.Show(); m.window.Focus() })
		m.window.OnMinimize(func() { m.minimized = true; m.samplerActive.Store(false) })
		m.window.OnRestore(func() {
			m.minimized = false
			m.samplerActive.Store(m.page == processPage)
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

// dbgMenu/dbgFrames gate stderr diagnostics for the input and frame paths
// while hunting UI stalls; off in normal runs.
var dbgMenu = os.Getenv("DG_DEBUG_MENU") != ""
var dbgFrames = os.Getenv("DG_DEBUG_FRAMES") != ""

func dbgLog(format string, args ...any) { log.Printf(format, args...) }

func (m *Model) view(c *ui.Context) {
	frameStart := time.Now()
	m.drain()
	if dbgFrames {
		if d := time.Since(frameStart); d > 150*time.Millisecond {
			dbgLog("drain took %v (events backlog)", d)
		}
	}
	if c.Shortcut(ui.Ctrl, ui.Key1) {
		m.setPage(processPage)
	}
	if c.Shortcut(ui.Ctrl, ui.Key2) {
		m.setPage(diskPage)
	}
	if c.Shortcut(ui.Ctrl, ui.Key3) {
		m.setPage(hardwarePage)
	}
	if c.Shortcut(ui.Ctrl, ui.Key4) {
		m.setPage(uninstallPage)
	}
	if c.Shortcut(ui.Ctrl, ui.Key5) {
		m.setPage(startupPage)
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
	m.samplerActive.Store(m.page == processPage && !m.minimized)
	if m.diskCancel != nil {
		c.After(100 * time.Millisecond)
	}
	// Live-index work continues on the next frames while the disk page is
	// on screen; hidden pages cost no frames — journal events keep
	// accumulating on the volume and replay on return.
	if m.page == diskPage {
		if len(m.diskPending) > 0 {
			c.After(50 * time.Millisecond)
		} else if m.diskLiveDirty {
			c.After(250 * time.Millisecond)
		} else {
			// One slow tick so freshly mounted drives appear on their own.
			c.After(3 * time.Second)
		}
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
	// The engine theme above styles our content; the caption buttons and the
	// window frame are native and read mygo.Theme's source instead. Sync it
	// once per change or the three buttons keep the system glyphs' color.
	if m.appliedThemeMode != m.themeMode {
		m.appliedThemeMode = m.themeMode
		switch m.themeMode {
		case 1:
			mygo.Theme.SetSource(mygo.ThemeLight)
		case 2:
			mygo.Theme.SetSource(mygo.ThemeDark)
		default:
			mygo.Theme.SetSource(mygo.ThemeSystem)
		}
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
		case hardwarePage:
			m.reservedView(c, "cpu", "Hardware", "Monitors CPU, memory, GPU, disk and network utilization with live charts. Tab position, shortcut and the shared search box are reserved.")
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
		title, msg := "End process?", fmt.Sprintf("%s (PID %d). Unsaved work may be lost.", m.terminateTarget.Name, m.terminateTarget.PID)
		if m.terminateTree {
			if n := m.processDescendants(m.terminateTarget.PID); n > 0 {
				title = "End process tree?"
				msg = fmt.Sprintf("%s (PID %d) and the %d processes under it. Unsaved work may be lost.", m.terminateTarget.Name, m.terminateTarget.PID, n)
			}
		}
		choice := ui.AlertDialog(c, &m.terminateOpen, title, msg, "Cancel", "End process")
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
	// the caption failed to appear: the floor reserves the whole three-button
	// strip (3×46) plus margin, so the last control can never slide beneath
	// the native buttons whichever frame the report arrives on.
	right := max(c.TitleBar().Right+10, 148)
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
			m.pageTab(c, t, diskPage, "Disk Usage")
			m.pageTab(c, t, hardwarePage, "Hardware")
			m.pageTab(c, t, uninstallPage, "Uninstall")
			m.pageTab(c, t, startupPage, "Startup")
		})
		ui.Spacer(c)
		switch m.page {
		case processPage:
			if ui.SearchField(c, &m.processQuery).Label("Search processes").Placeholder("Name, PID or path").Width(190).Changed() {
				m.dirtyProcesses = true
			}
		case diskPage:
			ui.SearchField(c, &m.diskQuery).Label("Filter files").Placeholder("Filter files").Width(190).Disabled(m.diskSnapshot == nil)
		default:
			ui.SearchField(c, &m.reservedQuery).Label("Search").Placeholder("Not available on this page").Width(190).Disabled(true)
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

// pageTab is one top-bar tab. It is a plain clickable row rather than
// ui.Tabs so the page change applies in the same frame as the click. The
// prototype ships two styles: "Underline" draws an accent line under the
// active tab; "Pill" gives it a solid accent face with white text.
func (m *Model) pageTab(c *ui.Context, t *ui.Theme, pg page, label string) {
	on := m.page == pg
	pill := m.tabStyle == "Pill"
	height, radius := float32(30), float32(4)
	if pill {
		height, radius = 28, 6
	}
	e := ui.Row(c).Height(height).PaddingX(8).AlignItems(ui.Center).Radius(radius).Label(label)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		switch {
		case on && pill:
			p.Fill(r, t.Accent, radius)
		case on:
			p.Fill(ui.Rect{X: r.X + 8, Y: r.Y + r.H - 2, W: r.W - 16, H: 2}, t.Accent, 1)
		case e.Hovered():
			p.Fill(r, t.SurfaceHover, radius)
		}
	})
	e.Children(func() {
		tx := ui.Text(c, label).SingleLine().FontSize(13)
		switch {
		case on && pill:
			tx.TextColor(t.AccentText)
		case on:
			tx.Bold()
		default:
			tx.TextColor(t.TextMuted)
		}
	})
	if e.Clicked() {
		m.setPage(pg)
	}
}

// setPage switches the top-level page. Coming back to Processes forces
// one sample so the table is never a stale frame behind.
func (m *Model) setPage(pg page) {
	if m.page == pg {
		return
	}
	m.page = pg
	if pg == processPage {
		m.refreshOnce()
	}
	if pg == diskPage {
		m.maybeRefreshVolumes()
	}
}

// maybeRefreshVolumes re-enumerates the drive chips at most every 3s —
// cheap enough to keep chips fresh, quiet enough to never churn.
func (m *Model) maybeRefreshVolumes() {
	if time.Since(m.volsChecked) > 3*time.Second {
		m.refreshVolumes()
	}
}

// refreshVolumes re-enumerates the drive chips off the main thread so a
// USB stick or a removed card reader appears, or disappears, while the
// disk page sits open — the startup scan alone missed hot-plugged media.
func (m *Model) refreshVolumes() {
	m.volsChecked = time.Now()
	if m.volsChecking.Swap(true) {
		return
	}
	go func() {
		defer m.volsChecking.Store(false)
		vols := disk.Volumes()
		m.post(func() {
			if len(vols) == 0 || slices.EqualFunc(vols, m.diskVolumes, func(a, b disk.Volume) bool {
				return a.Path == b.Path && a.Free == b.Free && a.Total == b.Total
			}) {
				return
			}
			m.diskVolumes = vols
		})
	}()
}

// selActs mirrors the prototype's #selacts: quick actions for the selected
// process live in the page toolbar. They stay visible — greyed out — when
// nothing is selected, so the row keeps its shape and the actions stay
// discoverable. Both end actions stay visible — no dropdown.
func (m *Model) selActs(c *ui.Context, t *ui.Theme) {
	selected, ok := m.selectedProcess()
	if m.pactButton(c, "folder", "Open location").Disabled(!ok || selected.Path == "").Clicked() {
		mygo.Shell.ShowItemInFolder(selected.Path)
	}
	if m.pactButton(c, "copy", "Copy path").Disabled(!ok || selected.Path == "").Clicked() {
		mygo.Clipboard.WriteText(selected.Path)
	}
	det := "chup"
	if m.showDetails {
		det = "down"
	}
	if m.iconTextButton(c, det, "Details").Clicked() {
		m.showDetails = !m.showDetails
	}
}

// selKillActs holds the destructive actions — pinned to the toolbar's
// right edge, away from the navigation-style buttons.
func (m *Model) selKillActs(c *ui.Context, t *ui.Theme) {
	selected, ok := m.selectedProcess()
	exited := ok && selected.Access == "Exited"
	killTip := "End process"
	switch {
	case !ok:
		killTip = "Select a process first"
	case exited:
		killTip = "Process already exited"
	}
	if m.dangerButton(c, "x", "End process").Disabled(!ok || selected.PID <= 4 || exited).Tooltip(killTip).Clicked() {
		m.terminateTarget, m.terminateTree, m.terminateOpen = selected, false, true
	}
	treeTip := "End the process and all its descendants"
	if !ok {
		treeTip = killTip
	} else if exited {
		treeTip = killTip
	}
	if m.dangerButton(c, "x", "End process tree").Disabled(!ok || selected.PID <= 4 || exited).Tooltip(treeTip).Clicked() {
		m.terminateTarget, m.terminateTree, m.terminateOpen = selected, true, true
	}
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

// dangerButton is the prototype's .btn.danger: the plain button face with
// the glyph and label in the theme's danger red.
func (m *Model) dangerButton(c *ui.Context, name, label string) ui.Element {
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
		icon(c, name, 14).TextColor(t.Danger)
		ui.Text(c, label).SingleLine().TextColor(t.Danger)
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
			// Once a snapshot exists its numbers live in the view bar; the
			// status bar only carries idle, scanning and error states.
			if m.diskSnapshot == nil {
				text = m.diskStatus
			}
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
		hint := "F5 refresh · Ctrl+1–5 pages"
		if m.page == diskPage {
			hint = "F5 scan · Backspace up · Ctrl+1–5 pages"
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
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, "Tab style")
					ui.Text(c, "Look of the page tabs in the top bar").FontSize(11).TextColor(c.Theme().TextMuted)
				})
				ui.Select(c, &m.tabStyle, []string{"Underline", "Pill"}).Width(100).Label("Tab style")
			})
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, "Memory after scan")
					ui.Text(c, "Return scan buffers to the OS, or keep them for faster rescans").FontSize(11).TextColor(c.Theme().TextMuted)
				})
				ui.Select(c, &m.memRelease, []string{"Automatic", "Keep for rescan"}).Width(110).Label("Memory after scan")
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
			ui.Text(c, "About the Scan Numbers").Bold().FontSize(16)
			if s := m.diskSnapshot; s != nil && s.Skipped > 0 {
				other := s.Skipped - s.Reparse - s.Offline - s.Unlinked
				body := fmt.Sprintf("This scan: %s total\n• %s links — junctions and symlinks (reparse points), skipped by design so nothing counts twice\n• %s cloud placeholders — offline stubs whose data is not on this disk\n• %s orphaned names — records whose parent was deleted or reused: deleted entries still sitting in the MFT\n• %s unreadable — corrupt records, denied folders, items deleted mid-scan",
					format.Int(s.Skipped), format.Int(s.Reparse), format.Int(s.Offline), format.Int(s.Unlinked), format.Int(other))
				m.diskInfoItem(c, "What Was Skipped", body)
			}
			m.diskInfoItem(c, "What Does \"Skipped\" Mean", "Entries the scan saw but did not count: access-denied folders, reparse points (junctions and links, skipped on purpose so nothing counts twice), cloud placeholders whose data is not on this disk, items deleted mid-scan, and records without a usable name. Skipped entries are simply absent — the totals you see only cover what could be read.")
			m.diskInfoItem(c, "Can They Be Scanned?", "Access-denied items mostly can: run DumpingGround as administrator and scan again. Cloud placeholders cannot — their content lives in the cloud, not on the disk. Reparse points stay skipped by design; scan their target folder instead.")
			m.diskInfoItem(c, "Estimates", "Files that were indexed but whose on-disk size had to be guessed, because no data attribute could be read. Their size is rounded up to a cluster.")
			m.diskInfoItem(c, "Live Index", "The index keeps itself current through the NTFS change journal: new, renamed, resized and deleted files update the numbers as they happen — no rescan needed. Live mode needs administrator rights; if the journal wraps under heavy churn the status line says \"Live index lost\" and a rescan restores it.")
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

// menuHit is one table row's vertical span in window coordinates, recorded
// by the row's first cell while the table builds.
type menuHit struct {
	y1, y2 float32
	id     int
}

// tableRowMenu resolves a right-click to a row through the spans the cells
// recorded last frame, then opens the row's menu. It replaces cell-local
// menus: they left the padding, gaps and indent wedges between contents
// without a menu element — clicks there looked ignored — and their element
// ids could go stale between the press and release frames, losing the
// release. The table element outlives both, so every click on a row opens
// the menu on the first try.
func tableRowMenu(el ui.Element, hits *[]menuHit, open func(id int, menu *ui.Menu)) {
	el.ContextMenu(func(menu *ui.Menu) {
		_, py, _ := el.PointerPosition()
		y := el.Bounds().Y + py
		const slop = 6 // spans cover the content, not the row's full height
		for _, h := range *hits {
			if y < h.y1-slop {
				break // spans are recorded in row order
			}
			if y < h.y2+slop {
				open(h.id, menu)
				return
			}
		}
	})
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
