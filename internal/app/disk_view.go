package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"runtime/debug"
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
	m.scanPath = path
	m.rustMode = false
	m.rustReport = nil
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
	m.extPending = nil
	m.extStats = nil
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
			m.diskStatus = fmt.Sprintf("Complete · %s · %s files · %s · %.1f s · %s skipped", r.snapshot.Method, format.Int(r.snapshot.Files), format.Bytes(r.snapshot.Allocated), r.snapshot.Duration.Seconds(), format.Int(r.snapshot.Skipped))
			m.startWatch()
			if m.diskLive {
				m.diskStatus += " · live"
			}
		}
		// Returning scan buffers to the OS makes the next rescan pay for
		// them again; keep them pooled when the user prefers speed.
		if m.memRelease == "Automatic" {
			go debug.FreeOSMemory()
		}
		if m.rustPending {
			m.rustPending = false
			m.rustAnalyze()
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
	m.diskPending = nil
	m.diskLiveDirty = false
	m.diskLiveFlush = time.Now()
	m.diskMapFlush = m.diskLiveFlush
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
	// Off the disk page the watch sleeps: the events channel backs up, the
	// reader blocks, and the journal keeps recording on the volume. Events
	// resume replaying, in order, the frame the page comes back.
	if s == nil || m.page != diskPage {
		return
	}
	for {
		select {
		case ev := <-m.diskEvents:
			m.diskPending = append(m.diskPending, ev)
		case err := <-m.diskWatchErr:
			m.diskStatus = "Live index lost: " + err.Error()
			m.stopWatch()
		default:
			goto apply
		}
	}
apply:
	preBytes, preAlloc := uint64(0), uint64(0)
	if m.diskRoot >= 0 && m.diskRoot < len(s.Nodes) {
		preBytes, preAlloc = s.Nodes[m.diskRoot].Bytes, s.Nodes[m.diskRoot].Allocated
	}
	changed := false
	var touched map[int]bool
	// Fold at most a few hundred events per frame — each restat costs a
	// syscall, and a busy journal must not stall the UI catching up.
	for budget := 300; len(m.diskPending) > 0 && budget > 0; {
		batch := m.diskPending[0]
		n := min(len(batch), budget)
		c, dirs := s.ApplyUSNDirs(batch[:n], nil)
		if c {
			changed = true
			if touched == nil {
				touched = map[int]bool{}
			}
			for id := range dirs {
				touched[id] = true
			}
		}
		budget -= n
		if m.diskPending[0] = batch[n:]; len(m.diskPending[0]) == 0 {
			m.diskPending = m.diskPending[1:]
		}
	}
	if changed {
		// Only a change inside the displayed subtree earns a rebuild:
		// the root's sums moved (size delta anywhere below) or a touched
		// directory sits inside the view (membership or rename).
		visible := false
		if m.diskRoot >= 0 && m.diskRoot < len(s.Nodes) {
			n := s.Nodes[m.diskRoot]
			visible = n.Bytes != preBytes || n.Allocated != preAlloc
		}
		for id := range touched {
			if visible {
				break
			}
			visible = m.diskInView(id)
		}
		if visible {
			m.diskLiveDirty = true
		}
	}
	if m.diskLiveDirty {
		now := time.Now()
		if now.Sub(m.diskLiveFlush) >= 750*time.Millisecond {
			m.diskViewKey = ""
			m.diskLiveFlush = now
			m.diskLiveDirty = false
			// The map's extension stats re-walk the whole subtree; refresh
			// it on a slower cadence than the cheap row list.
			if now.Sub(m.diskMapFlush) >= 3*time.Second {
				m.mapRoot = -1
				m.diskMapFlush = now
			}
		}
	}
}

// diskInView reports whether id lies inside the subtree on screen —
// it is the displayed root or one of its descendants.
func (m *Model) diskInView(id int) bool {
	s := m.diskSnapshot
	for p, steps := id, 0; p >= 0 && steps <= len(s.Nodes); steps++ {
		if p == m.diskRoot {
			return true
		}
		p = s.Nodes[p].Parent
	}
	return false
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
	m.diskPending = nil
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
	m.extPending = nil
	m.extStats = nil
	m.mapHover = ""
	m.mapMotion = mapMotion{}
	m.mapAnimating = false
	m.rustPending = false
	m.rustOwnsIndex = false
	m.rustSel = -1
	m.rustConfirm = -1
	m.rustRowConfirm = ""
	m.rustFilterCat = -1
	m.rustPathsCat = -1
	m.rustPathsOpen = false
	m.diskStatus = "Index cleared"
	// Dropping the index frees hundreds of MB; return them to the OS now
	// instead of letting the runtime sit on the released heap. Async so the
	// sweep does not stall the UI.
	go debug.FreeOSMemory()
}

// releaseIndexForRust drops the index a Rust-only scan built while keeping
// the self-contained report up — the report's paths and sizes do not need
// the tree that measured them, and neither does deleting the dirs.
func (m *Model) releaseIndexForRust() {
	m.stopWatch()
	m.diskPending = nil
	m.diskLiveDirty = false
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
	m.extPending = nil
	m.extStats = nil
	m.mapHover = ""
	m.mapMotion = mapMotion{}
	m.mapAnimating = false
	m.diskStatus = "Rust report ready · index released"
	go debug.FreeOSMemory()
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
	m.maybeRefreshVolumes()
	scanning := m.diskCancel != nil
	ui.Row(c).FillWidth().Height(44).Shrink(0).PaddingX(12).Gap(8).AlignItems(ui.Center).Children(func() {
		for _, volume := range m.diskVolumes {
			m.driveChip(c, t, volume, scanning)
		}
		ui.Spacer(c)
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
		if m.rustButton(c, m.rustBusy || scanning && !m.rustPending).Clicked() {
			m.rustScan()
		}
	})
	if scanning {
		m.scanningCard(c, t)
		return
	}
	// The Rust report is self-contained and checked first — it can be up
	// with no index at all after a Rust-only scan releases its snapshot.
	if m.rustMode && m.rustReport != nil {
		m.rustResultsView(c, t)
		return
	}
	if m.rustBusy && m.rustReport == nil {
		// The scan just settled and artifact analysis is running — skip the
		// one-frame treemap flash before the report lands.
		m.rustAnalyzingView(c, t)
		return
	}
	if m.diskSnapshot == nil {
		m.idleCard(c, t)
		return
	}
	if m.indexIsStale() {
		// The index belongs to another drive — showing it under this one's
		// chip would lie about what is scanned.
		m.unscannedView(c, t)
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
			p.Fill(r, t.Accent, 6)
		} else if e.Hovered() && !scanning {
			p.Fill(r, t.SurfaceHover, 6)
		}
	})
	e.Children(func() {
		fg, muted := t.Text, t.TextMuted
		if on {
			fg, muted = t.AccentText, t.AccentText.Alpha(0.85)
		}
		ui.Text(c, v.Path).Bold().SingleLine().Ellipsis("…").MaxWidth(140).TextColor(fg)
		used := 0.0
		if v.Total > 0 {
			used = float64(v.Total-v.Free) / float64(v.Total)
		}
		if on {
			// A white mini bar reads on the accent face where the accent
			// progress fill would disappear.
			ui.Box(c).Width(34).Height(4).Radius(2).Clip().Background(t.AccentText.Alpha(0.35)).Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(min(used, 1)), H: r.H}, t.AccentText, 2)
			})
		} else {
			ui.Progress(c, used).Width(34).Height(4)
		}
		ui.Text(c, format.Bytes(v.Free)+" free").FontSize(11).TextColor(muted).SingleLine()
	})
	if !scanning && e.Clicked() {
		m.selectDrive(v.Path)
	}
}

// idleCard is the centered placeholder the page shows before the first scan.
func (m *Model) idleCard(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		custom := true
		ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
			for _, v := range m.diskVolumes {
				if v.Path == m.diskPath {
					custom = false
				}
				m.driveCard(c, t, v)
			}
			if custom {
				card := ui.Column(c).Width(280).Padding(16, 20).Gap(10).Background(t.Surface).Radius(8)
				card.BorderWidth(1).BorderColor(t.Accent)
				card.Children(func() {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
						icon(c, "folder", 26).TextColor(t.TextMuted)
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, "Custom folder").FontSize(14).Bold().SingleLine()
							ui.Text(c, m.diskPath).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
						})
					})
				})
			}
		})
		ui.Text(c, "Pick a drive or browse for a folder, then Scan (F5). Scans run in the background and can be cancelled.").PaddingY(12).TextColor(t.TextMuted).SingleLine()
		ui.Spacer(c)
	})
}

// driveCard is one big volume card on the idle page: title, Used/Free/Total
// and a fill bar. Clicking selects the drive, like its chip above.
func (m *Model) driveCard(c *ui.Context, t *ui.Theme, v disk.Volume) {
	on := m.diskPath == v.Path
	card := ui.Column(c).Width(280).Padding(16, 20).Gap(10).Background(t.Surface).Radius(8)
	card.BorderWidth(1).BorderColor(t.Border).Cursor(ui.CursorPointer)
	if on {
		card.BorderColor(t.Accent)
	}
	card.Label("Drive card " + v.Path).Tooltip("Scan " + v.Path)
	card.Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			icon(c, "hdd", 26).TextColor(t.TextMuted)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, v.Path+"  "+v.Label).FontSize(14).Bold().SingleLine()
				note := v.FS + " · directory walk"
				if v.FS == "NTFS" {
					note = "NTFS · read-only MFT path"
				} else if v.FS == "" {
					note = "Directory walk"
				}
				ui.Text(c, note).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
			})
		})
		ui.Row(c).Gap(24).Children(func() {
			m.stat(c, t, "Used", format.Bytes(v.Total-v.Free))
			m.stat(c, t, "Free", format.Bytes(v.Free))
			m.stat(c, t, "Total", format.Bytes(v.Total))
		})
		ui.Progress(c, float64(v.Total-v.Free)/float64(max(v.Total, 1))).FillWidth().Height(6)
	})
	if card.Clicked() {
		m.selectDrive(v.Path)
	}
}

// selectDrive picks a volume without starting a scan — leaving results up
// (or picking another drive mid-review) never rescans on its own; the
// Scan button does. Returning to the drive whose index produced the Rust
// report puts that report back up.
func (m *Model) selectDrive(path string) {
	m.diskDrive = path
	m.diskPath = path
	m.rustMode = m.rustReport != nil && strings.EqualFold(m.scanPath, path)
}

// indexIsStale reports whether the loaded index was built from a path
// other than the selected one. An empty scanPath means none was recorded —
// injected snapshots in tests — and counts as fresh.
func (m *Model) indexIsStale() bool {
	return m.scanPath != "" && !strings.EqualFold(m.scanPath, strings.TrimSpace(m.diskPath))
}

// unscannedView stands in for results while a different drive's index is
// still loaded — the selected drive just has not been scanned yet.
func (m *Model) unscannedView(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		ui.Column(c).Width(340).Padding(18, 24).Gap(12).AlignItems(ui.Center).Background(t.Surface).Radius(8).BorderWidth(1).BorderColor(t.Border).Children(func() {
			icon(c, "hdd", 30).TextColor(t.TextMuted)
			ui.Text(c, m.diskPath+" hasn't been indexed").FontSize(14).Bold().SingleLine().Ellipsis("…").Tooltip(m.diskPath)
			ui.Text(c, "Scan it to see what's inside — the previous index stays untouched until you do.").FontSize(11).TextColor(t.TextMuted)
			if ui.PrimaryButton(c, "Scan "+m.diskPath).Clicked() {
				m.startScan()
			}
		})
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
		card := ui.Column(c).Width(560).Padding(16, 20).Gap(10).Background(t.Surface).Radius(8)
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
				m.stat(c, t, "Files", format.Int(p.Files))
				m.stat(c, t, "Folders", format.Int(p.Dirs))
				m.stat(c, t, "Tallied", format.Bytes(p.Allocated))
				m.stat(c, t, "Elapsed", fmt.Sprintf("%.1f s", time.Since(m.diskStarted).Seconds()))
				m.stat(c, t, "Skipped", format.Int(p.Skipped))
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
		// The breadcrumb follows the selection: with a node picked it is the
		// path to that node, clickable segment by segment; otherwise it shows
		// the directory being browsed.
		m.breadcrumbs(c, t, s, m.diskNode)
		if m.diskNode >= 0 && m.diskNode < len(s.Nodes) {
			ui.Text(c, format.Bytes(m.diskValue(s.Nodes[m.diskNode]))).TextColor(t.TextMuted).Shrink(0)
		} else {
			hover := m.mapHover
			if hover == "" {
				hover = "Click a folder to zoom · click a file to locate its row"
			}
			ui.Text(c, hover).SingleLine().Grow(1).MinWidth(0).FontSize(11).TextColor(t.TextMuted).Ellipsis("…")
		}
		ui.Spacer(c)
		root := s.Nodes[m.diskRoot]
		ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, fmt.Sprintf("Allocated %s · Logical %s · %s files ·", format.Bytes(root.Allocated), format.Bytes(root.Bytes), format.Int(s.Files))).FontSize(11).TextColor(t.TextMuted).SingleLine()
			skip := ui.Row(c).Gap(3).Padding(1, 5).Radius(4).AlignItems(ui.Center).Label("What is skipped").Tooltip("Entries the scan could not index — click for details")
			skip.Draw(func(p *ui.Painter, r ui.Rect) {
				if skip.Hovered() {
					p.Fill(r, t.SurfaceHover, 4)
				}
			})
			skip.Children(func() {
				ui.Text(c, format.Int(s.Skipped)+" skipped").FontSize(11).TextColor(t.TextMuted).SingleLine()
				icon(c, "info", 11).TextColor(t.Accent)
			})
			if skip.Clicked() {
				m.diskInfoOpen = true
			}
			ui.Text(c, "· "+format.Int(s.Estimated)+" estimates").FontSize(11).TextColor(t.TextMuted).SingleLine().Tooltip("estimates: files whose on-disk size had to be guessed, e.g. no data attribute read")
			// Scan facts that used to sit in the status bar: method, duration
			// and the live-index state.
			state := s.Method + fmt.Sprintf(" · %.1f s", s.Duration.Seconds())
			switch {
			case strings.HasPrefix(m.diskStatus, "Live index lost"):
				state += " · index lost"
			case m.diskLive:
				state += " · live"
			}
			ui.Text(c, "· "+state).FontSize(11).TextColor(t.TextMuted).SingleLine().Tooltip(m.diskStatus)
		})
		if m.extensionFilter != "" {
			m.extChip(c, t)
		}
		if m.diskNode >= 0 && m.diskNode < len(s.Nodes) {
			path := s.FullPath(m.diskNode)
			if m.iconTextButton(c, "folder", "Open location").Clicked() && m.window != nil {
				mygo.Shell.ShowItemInFolder(path)
			}
			if m.iconTextButton(c, "copy", "Copy path").Clicked() && m.window != nil {
				mygo.Clipboard.WriteText(path)
			}
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
		if m.iconTextButton(c, "trash", "Clear index").Tooltip("Drop the index and return its memory to Windows").Clicked() {
			m.clearDisk()
		}
		ui.Checkbox(c, &m.animateMap, "Animate").Tooltip("Zoom transitions when drilling the space map")
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
func (m *Model) breadcrumbs(c *ui.Context, t *ui.Theme, s *disk.Snapshot, to int) {
	if to < 0 || to >= len(s.Nodes) {
		to = m.diskRoot
	}
	var chain []int
	for id := to; id >= 0 && len(chain) <= len(s.Nodes); id = s.Nodes[id].Parent {
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
	e := ui.Row(c).Padding(2, 6).Radius(4).MaxWidth(200).Label(label)
	if !last {
		e.Cursor(ui.CursorPointer).Tooltip(label)
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
	chip.Draw(func(p *ui.Painter, r ui.Rect) { p.Fill(r, t.Accent, 12) })
	chip.Children(func() {
		ui.Box(c).Size(8, 8).Radius(2).Background(extensionColor(m.extensionFilter))
		ui.Text(c, m.extensionFilter).FontSize(11).SingleLine().TextColor(t.AccentText)
		x := ui.Row(c).Size(16, 16).Center().Radius(4).Label("Clear type filter").Tooltip("Clear type filter")
		x.Draw(func(p *ui.Painter, r ui.Rect) {
			if x.Hovered() {
				p.Fill(r, t.AccentText.Alpha(0.25), 4)
			}
		})
		x.Children(func() { icon(c, "x", 10).TextColor(t.AccentText) })
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
		seen := map[int]bool{m.diskRoot: true}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n := s.Nodes[id]
			if n.Dir {
				for _, k := range n.Children {
					if !seen[k] {
						seen[k] = true
						stack = append(stack, k)
					}
				}
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
	m.diskMenuHits = m.diskMenuHits[:0]
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
				}).Tooltip(s.FullPath(id))
				return
			}
		case 4:
			text = s.FullPath(id)
			if e := s.ErrorAt(id); e != "" {
				text = e
			}
		}
		e := ui.Text(c, text).SingleLine().Tooltip(s.FullPath(id))
		if col == 0 {
			if b := e.Bounds(); b.H > 0 {
				m.diskMenuHits = append(m.diskMenuHits, menuHit{y1: b.Y, y2: b.Y + b.H, id: id})
			}
		}
	}
	selectedID := -1
	submitted := false
	if m.diskMode == "Tree" && m.diskQuery == "" && m.extensionFilter == "" {
		m.diskTree.List.Selected = &m.diskSelected
		m.diskTree.List.Sort = &m.diskSort
		tbl := tableChrome(c, func() ui.Element {
			return ui.OutlineTable(c, &m.diskTree, cols, m.diskRows, func(id int) []int { return s.Nodes[id].Children }, cell)
		}).Fill().Label("Disk table")
		tableRowMenu(tbl, &m.diskMenuHits, func(id int, menu *ui.Menu) { m.diskMenu(menu, id) })
		submitted = tbl.Submitted()
		if m.diskSelected >= 0 && m.diskSelected < m.diskTree.Rows() {
			selectedID = m.diskTree.Item(m.diskSelected)
		}
	} else {
		m.diskFiles.Selected = &m.diskSelected
		m.diskFiles.Sort = &m.diskSort
		m.diskFiles.Key = func(i int) any { return m.diskRows[i] }
		tbl := tableChrome(c, func() ui.Element {
			return ui.Table(c, &m.diskFiles, cols, len(m.diskRows), func(i, col int) { cell(m.diskRows[i], col) })
		}).Fill().Label("Disk table")
		tableRowMenu(tbl, &m.diskMenuHits, func(id int, menu *ui.Menu) { m.diskMenu(menu, id) })
		submitted = tbl.Submitted()
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
	// Children arrive sorted by allocated size, so the tail is the small
	// fry: fold it into one "Other" block instead of painting thousands of
	// hairline rectangles every frame.
	const maxMapItems = 1500
	if len(m.diskItems) > maxMapItems {
		rest := uint64(0)
		for _, it := range m.diskItems[maxMapItems:] {
			rest += it.Value
		}
		m.diskItems = m.diskItems[:maxMapItems]
		if rest > 0 {
			m.diskItems = append(m.diskItems, treemap.Item{ID: -1, Value: rest})
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
	// The extension table needs a full subtree walk — far too slow for one
	// frame at a big root. Seed the walk here and let pumpExtensions chew
	// through it a bounded slice per frame.
	m.extStats = map[string]extensionStat{}
	m.extPending = m.extPending[:0]
	m.extPending = append(m.extPending, m.diskRoot)
}

// pumpExtensions advances the subtree walk feeding the extension table by
// a bounded slice per frame. A node has exactly one parent, so no seen-set
// is needed — a directory can never appear twice in a children traversal.
func (m *Model) pumpExtensions(c *ui.Context) {
	s := m.diskSnapshot
	if s == nil || len(m.extPending) == 0 {
		return
	}
	const budget = 150_000
	for n := 0; n < budget && len(m.extPending) > 0; n++ {
		id := m.extPending[len(m.extPending)-1]
		m.extPending = m.extPending[:len(m.extPending)-1]
		node := s.Nodes[id]
		if node.Dir {
			m.extPending = append(m.extPending, node.Children...)
			continue
		}
		ext := fileExtension(node.Name)
		st := m.extStats[ext]
		st.Extension = ext
		st.Files++
		st.Bytes += m.diskValue(node)
		m.extStats[ext] = st
	}
	if len(m.extPending) > 0 {
		c.AnimationFrame()
		return
	}
	m.diskExtensions = m.diskExtensions[:0]
	for _, st := range m.extStats {
		m.diskExtensions = append(m.diskExtensions, st)
	}
	slices.SortFunc(m.diskExtensions, func(a, b extensionStat) int { return cmp.Compare(b.Bytes, a.Bytes) })
	m.extStats = nil
}

func (m *Model) diskMap(c *ui.Context) {
	m.prepareMap()
	t := c.Theme()
	animate := m.animateMap && !c.Preferences().ReduceMotion
	ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
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
				// Only navigation animates — zooming in or out. Live index
				// updates resize blocks in place; pulsing them every flush
				// reads as flicker, so data changes snap.
				switch {
				case animate && old.Root != next.Root:
					m.mapMotion.retarget(next, p.Now(), true, enter, back)
				case m.mapAnimating && len(m.mapMotion.From) > 0:
					// A live relayout during a zoom must not kill the
					// transition: freeze the in-flight positions and re-aim
					// each rect at its new destination on the same clock.
					v := mapEase(m.mapMotion.progress(p.Now(), animate))
					frozen := make(map[int]treemap.Rect, len(m.mapMotion.From))
					for i, f := range m.mapMotion.From {
						frozen[m.mapMotion.Scene.Rects[i].ID] = interpolateRect(f, m.mapMotion.Scene.Rects[i], v)
					}
					m.mapMotion.From = m.mapMotion.From[:0]
					for _, r := range next.Rects {
						from, ok := frozen[r.ID]
						if !ok {
							from = r
							from.X += r.W / 2
							from.Y += r.H / 2
							from.W, from.H = 0, 0
						}
						m.mapMotion.From = append(m.mapMotion.From, from)
					}
					m.mapMotion.Scene = next
				default:
					m.mapMotion.retarget(next, p.Now(), false, -1, -1)
				}
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
	if alpha <= 0 || m.diskSnapshot == nil || b.ID >= len(m.diskSnapshot.Nodes) {
		return
	}
	x, y := max(b.X, 0), max(b.Y, 0)
	w, h := min(b.X+b.W-1, viewport.W)-x, min(b.Y+b.H-1, viewport.H)-y
	if w < 1 || h < 1 {
		return
	}
	if b.ID < 0 {
		// The "Other files" aggregate of the small fry tail.
		box := ui.Rect{X: viewport.X + x, Y: viewport.Y + y, W: w, H: h}
		p.Fill(box, ui.Hex("#9a9aa0").Alpha(alpha), 2)
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
	m.pumpExtensions(c)
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
			text := format.Int(s.Files)
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
