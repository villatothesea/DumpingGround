package app

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/dumpingground/dumpingground/internal/disk"
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
	mygo.App.SetName("DumpingGround")
	if !mygo.App.RequestSingleInstanceLock() {
		m.close()
		return nil
	}
	mygo.App.WhenReady(func() {
		if m.InitialPage == "disk" {
			m.page = diskPage
		}
		m.iconCache[""] = systemIcon("")
		go m.loadIcons()
		m.window = mygo.NewWindow(mygo.WindowOptions{Title: "DumpingGround", Width: 1180, Height: 760, MinWidth: 880, MinHeight: 580, Content: ui.View(m.view)})
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
}
func (m *Model) view(c *ui.Context) {
	m.drain()
	if c.Shortcut(ui.Ctrl, ui.Key1) {
		m.page = processPage
	}
	if c.Shortcut(ui.Ctrl, ui.Key2) {
		m.page = diskPage
	}
	if c.Shortcut(0, ui.KeyF5) {
		if m.page == processPage {
			m.refreshOnce()
		} else {
			m.startScan()
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
	t := c.Theme()
	ui.Column(c).Fill().AlignItems(ui.Stretch).Font("Segoe UI").FontSize(12).Background(t.Background).Children(func() {
		ui.Row(c).FillWidth().Height(42).Shrink(0).Padding(5, 10).Gap(8).AlignItems(ui.Center).Background(t.Surface).Children(func() {
			ui.Text(c, "DumpingGround").FontSize(16).Bold().Width(155)
			p := ui.Button(c, "Processes").Label("Processes")
			if m.page == processPage {
				p.Background(t.Accent).TextColor(t.AccentText)
			}
			if p.Clicked() {
				m.page = processPage
			}
			d := ui.Button(c, "Disk usage").Label("Disk usage")
			if m.page == diskPage {
				d.Background(t.Accent).TextColor(t.AccentText)
			}
			if d.Clicked() {
				m.page = diskPage
			}
			ui.Spacer(c)
			ui.Text(c, fmt.Sprintf("%d processes", len(m.processRows))).FontSize(12).TextColor(t.TextMuted)
		})
		if m.page == processPage {
			m.processView(c)
		} else {
			m.diskView(c)
		}
		ui.Row(c).FillWidth().Height(26).Shrink(0).Padding(4, 10).Gap(8).AlignItems(ui.Center).Background(t.Surface).Children(func() {
			text := m.status
			if text == "" {
				text = "F5 Refresh   ·   Ctrl+1 Processes   ·   Ctrl+2 Disk usage"
			}
			if m.page == diskPage {
				text = m.diskStatus
			}
			ui.Text(c, text).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextMuted)
		})
	})
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
