package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dumpingground/dumpingground/internal/applog"
	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/dumpingground/dumpingground/internal/uninstall"
	"github.com/egoist/mygo/ui"
)

// uninstallView lists installed software from the registry's Uninstall
// keys. Reading is a one-shot registry walk — cheap enough to re-run
// on demand rather than cache against the process sampler.
func (m *Model) uninstallView(c *ui.Context, t *ui.Theme) {
	if !m.uninstLoaded && !m.uninstLoading {
		m.uninstLoading = true
		go func() {
			rows := uninstall.List()
			m.post(func() {
				m.uninstLoading = false
				m.uninstLoaded = true
				m.uninstRows = rows
			})
		}()
	}
	ui.Column(c).Fill().Padding(4, 12).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Height(30).AlignItems(ui.Center).Gap(8).Children(func() {
			icon(c, "pkg", 18)
			var total uint64
			for i := range m.uninstRows {
				total += m.uninstRows[i].SizeBytes
			}
			head := "Uninstall"
			if m.uninstLoaded {
				head = fmt.Sprintf("Uninstall — %s apps · %s installed", format.Int(uint64(len(m.uninstRows))), format.Bytes(total))
			}
			ui.Text(c, head).Bold().Shrink(0)
			if m.uninstLoading {
				ui.Text(c, "reading the registry…").FontSize(11).TextColor(t.TextMuted)
			} else if m.uninstMsg != "" {
				ui.Text(c, m.uninstMsg).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			}
			ui.Spacer(c)
			status := "Uninstall launches the app's own uninstaller — nothing is deleted directly."
			if m.uninstBusy != "" {
				status = "Waiting for " + m.uninstBusy + "'s uninstaller — rows unlock when it exits."
			}
			ui.Text(c, status).FontSize(11).TextColor(t.TextMuted).SingleLine()
			broom := m.iconButton(c, "broom", "Scan for leftovers of apps already removed")
			if m.orphanScanning {
				broom = broom.Disabled(true)
			}
			if broom.Clicked() {
				m.startOrphanScan()
			}
			if m.iconButton(c, "refresh", "Re-read the installed-apps list").Clicked() {
				m.uninstLoaded = false
			}
		})
		// A leftover review that outlived the previous run reopens
		// once — the file under %LOCALAPPDATA% is written when the
		// modal opens and cleared only by Remove or "Keep leftovers".
		if m.uninstLoaded && !m.sweepRestored {
			m.sweepRestored = true
			if name, items := uninstall.LoadPending(); name != "" {
				m.sweepName, m.sweepItems = name, items
				m.sweepSel = make([]bool, len(items))
				for i := range items {
					m.sweepSel[i] = true
				}
				m.sweepOpen, m.sweepArmed = true, false
				m.uninstMsg = name + " — leftover review resumed from before the app was closed"
			}
		}
		rows := m.uninstRowsFiltered()
		if len(rows) == 0 {
			msg := "Reading installed apps…"
			if m.uninstLoaded {
				msg = "Nothing installed — the registry lists no apps."
				if m.uninstQuery != "" {
					msg = "No apps match this filter."
				}
			}
			ui.Text(c, msg).TextColor(t.TextMuted)
			return
		}
		rustSectionLabel(c, t, "Installed applications")
		m.sweepModal(c, t)
		if m.uninstList.Sort == nil {
			m.uninstSort = ui.SortOrder{Column: "Size", Descending: true}
			m.uninstList.Sort = &m.uninstSort
		}
		// ui.Table owns the header: columns resize at the edges,
		// double-click an edge to fit content, extra width scrolls.
		m.uninstMenuHits = m.uninstMenuHits[:0]
		m.uninstList.Selected = &m.uninstSelected
		cols := []ui.TableColumn{
			{Title: "Name", Sortable: true, MinWidth: 140},
			{Title: "Version", Width: 120, MinWidth: 60, Sortable: true},
			{Title: "Publisher", Width: 230, MinWidth: 60, Sortable: true},
			{Title: "Size", Width: 80, MinWidth: 50, Align: ui.End, Sortable: true},
			{Title: "Installed", Width: 90, MinWidth: 70, Align: ui.End, Sortable: true},
			{Title: "Install location", MinWidth: 240, Sortable: true},
			{Title: "", ID: "actions", Width: 152, Fixed: true, Align: ui.End},
		}
		tbl := tableChrome(c, func() ui.Element {
			return ui.Table(c, &m.uninstList, cols, len(rows), func(row, col int) {
				e := rows[row]
				cell := ui.Row(c).Fill().Height(26).Gap(6).AlignItems(ui.Center)
				if col == 0 {
					if b := cell.Bounds(); b.H > 0 {
						m.uninstMenuHits = append(m.uninstMenuHits, menuHit{y1: b.Y, y2: b.Y + b.H, id: row})
					}
				}
				cell.Children(func() {
					switch col {
					case 0:
						if bmp := m.processIcon(e.IconPath()); bmp != nil {
							ui.Image(c, bmp).Size(16, 16).Shrink(0)
						} else {
							icon(c, "pkg", 13).TextColor(t.TextMuted)
						}
						ui.Text(c, e.Name).SingleLine().Tooltip(e.Name + "  " + e.Version)
					case 1:
						ui.Text(c, e.Version).FontSize(11).SingleLine().Tooltip(e.Version)
					case 2:
						ui.Text(c, e.Publisher).FontSize(11).TextColor(t.TextMuted).SingleLine().Tooltip(e.Publisher)
					case 3:
						sz := "—"
						if e.EstimatedSize {
							sz = format.Bytes(e.SizeBytes)
						}
						ui.Text(c, sz).FontSize(11)
					case 4:
						d := e.Date()
						if d == "" {
							d = "—"
						}
						ui.Text(c, d).FontSize(11).TextColor(t.TextMuted).Tooltip(e.InstallDate)
					case 5:
						ui.Text(c, e.Location).FontSize(10).TextColor(t.TextMuted).SingleLine().Tooltip(e.Location)
					default:
						m.uninstRowActions(c, t, e)
					}
				})
			}).Grow(1).MinHeight(0).FillWidth()
		}).Grow(1).MinHeight(0).FillWidth()
		tableRowMenu(tbl, &m.uninstMenuHits, func(id int, menu *ui.Menu) {
			m.uninstSelected = id
			m.uninstMenu(c, menu, rows[id])
		})
	})
}

// uninstRowsFiltered applies the search box and the column sort.
func (m *Model) uninstRowsFiltered() []*uninstall.Entry {
	q := strings.ToLower(strings.TrimSpace(m.uninstQuery))
	var out []*uninstall.Entry
	for i := range m.uninstRows {
		e := &m.uninstRows[i]
		if q != "" &&
			!strings.Contains(strings.ToLower(e.Name), q) &&
			!strings.Contains(strings.ToLower(e.Publisher), q) &&
			!strings.Contains(strings.ToLower(e.Version), q) {
			continue
		}
		out = append(out, e)
	}
	less := func(a, b int) bool {
		ra, rb := out[a], out[b]
		var lt bool
		switch m.uninstSort.Column {
		case "Name":
			lt = strings.ToLower(ra.Name) < strings.ToLower(rb.Name)
		case "Version":
			lt = ra.Version < rb.Version
		case "Publisher":
			lt = strings.ToLower(ra.Publisher) < strings.ToLower(rb.Publisher)
		case "Installed":
			lt = ra.InstallDate < rb.InstallDate
		case "Install location":
			lt = strings.ToLower(ra.Location) < strings.ToLower(rb.Location)
		default: // "Size"
			lt = ra.SizeBytes < rb.SizeBytes
		}
		if m.uninstSort.Descending {
			return !lt
		}
		return lt
	}
	sort.SliceStable(out, less)
	return out
}

// uninstRowActions renders the actions cell's content: open the
// install folder, and an armed two-click Uninstall that launches the
// app's own uninstaller (which brings its own confirmation UI).
func (m *Model) uninstRowActions(c *ui.Context, t *ui.Theme, e *uninstall.Entry) {
	ui.Row(c).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
		// The folder button always renders — grayed without a location —
		// so the Uninstall button sits at the same offset on every row.
		op := ui.Row(c).Height(22).PaddingX(7).Gap(4).AlignItems(ui.Center).Radius(t.Radius).
			Cursor(ui.CursorPointer).Label("Open install folder")
		if e.Location == "" {
			op = op.Disabled(true).Tooltip("no install location recorded in the registry")
		} else {
			op = op.Tooltip(e.Location)
		}
		op.BorderWidth(1).BorderColor(t.Border).Background(t.Surface)
		op.Draw(func(p *ui.Painter, r ui.Rect) {
			if e.Location != "" && op.Hovered() {
				p.Fill(r, t.SurfaceHover, t.Radius)
			}
		})
		op.Children(func() {
			ic := icon(c, "folder", 11)
			if e.Location == "" {
				ic.TextColor(t.TextMuted)
			} else {
				ic.TextColor(t.Accent)
			}
		})
		if op.Clicked() && e.Location != "" {
			_ = exec.Command("explorer", e.Location).Start()
		}
		armed := m.uninstArmed == e.Name
		label := "Uninstall"
		if armed {
			label = "Sure?"
		}
		busy := m.uninstBusy != ""
		u := ui.Row(c).Height(22).PaddingX(8).Gap(4).AlignItems(ui.Center).Radius(t.Radius).
			Cursor(ui.CursorPointer).Label("Uninstall " + e.Name).
			Tooltip("Launch the app's own uninstaller: " + e.Uninstall)
		u.BorderWidth(1).BorderColor(t.Border).Background(t.Surface)
		if busy {
			u.Cursor(ui.CursorDefault).Tooltip("Another uninstaller is running")
		}
		if armed {
			u.Background(t.Danger.Alpha(0.12)).BorderColor(t.Danger)
		}
		u.Draw(func(p *ui.Painter, r ui.Rect) {
			if busy {
				return
			}
			if u.Pressed() {
				p.Fill(r, t.SurfacePressed, t.Radius)
			} else if u.Hovered() {
				p.Fill(r, t.SurfaceHover, t.Radius)
			}
		})
		u.Children(func() {
			icon(c, "trash", 11).TextColor(t.Danger)
			ui.Text(c, label).FontSize(11).TextColor(t.Danger).SingleLine()
		})
		if u.Clicked() && !busy {
			if !armed {
				m.uninstArmed = e.Name
				return
			}
			m.uninstArmed = ""
			m.launchUninstall(e)
		}
	})
}

// uninstMenu fills the row's right-click menu for one installed app:
// the same Uninstall flow, folder/registry shortcuts, a standalone
// leftover scan, and clipboard copies of the raw registry data.
func (m *Model) uninstMenu(c *ui.Context, mm *ui.Menu, e *uninstall.Entry) {
	if mm.Item("Uninstall").Disabled(m.uninstBusy != "").Chosen() {
		m.uninstArmed = ""
		m.launchUninstall(e)
	}
	mm.Separator()
	if mm.Item("Open install folder").Disabled(e.Location == "").Chosen() {
		_ = exec.Command("explorer", e.Location).Start()
	}
	if mm.Item("Find leftovers").Chosen() {
		m.startSweep(e)
	}
	mm.Separator()
	if mm.Item("Copy uninstall command").Chosen() {
		c.WriteClipboard(e.Uninstall)
	}
	if mm.Item("Copy details").Chosen() {
		c.WriteClipboard(fmt.Sprintf("%s %s — %s\nInstalled: %s\nLocation: %s\n%s",
			e.Name, e.Version, e.Publisher, e.Date(), e.Location, e.Uninstall))
	}
	mm.Separator()
	if mm.Item("Open in Registry Editor").Chosen() {
		if err := e.OpenInRegedit(); err != nil {
			m.uninstMsg = "regedit: " + err.Error()
		}
	}
}

// startSweep scans an entry's leftovers without uninstalling it — the
// "Find leftovers" menu action. Same review modal as the post-uninstall
// flow.
func (m *Model) startSweep(e *uninstall.Entry) {
	m.sweepName, m.sweepArmed = e.Name, false
	all := append([]uninstall.Entry(nil), m.uninstRows...)
	entry := *e
	go func() {
		items := uninstall.Footprint(&entry, all)
		m.post(func() {
			m.sweepItems = items
			m.sweepSel = make([]bool, len(items))
			for i := range items {
				m.sweepSel[i] = true
			}
			uninstall.SavePending(entry.Name, items)
			m.sweepOpen = true
		})
	}()
}

// orphanScanName titles the leftover review that aggregates several
// already-removed apps — each item's Why carries its own context.
const orphanScanName = "Removed apps"

// startOrphanScan looks for leftovers of software that is already
// gone — dead shortcuts, dead startup entries, stale associations,
// empty Start Menu groups — plus name-matched traces of apps this tool
// uninstalled before (their names live in uninstalled.json and past
// logs). The same review modal shows what it finds.
func (m *Model) startOrphanScan() {
	if m.sweepOpen || m.orphanScanning || m.uninstBusy != "" {
		return
	}
	m.orphanScanning = true
	m.uninstMsg = "Scanning for leftovers of removed apps…"
	all := append([]uninstall.Entry(nil), m.uninstRows...)
	go func() {
		items := uninstall.Orphans(all)
		applog.Logf("orphan scan: %d items", len(items))
		m.post(func() {
			m.orphanScanning = false
			if len(items) == 0 {
				m.uninstMsg = "No leftovers of removed apps found"
				return
			}
			m.sweepName = orphanScanName
			m.sweepItems = items
			m.sweepSel = make([]bool, len(items))
			for i := range items {
				m.sweepSel[i] = true
			}
			uninstall.SavePending(orphanScanName, items)
			m.sweepOpen, m.sweepArmed = true, false
		})
	}()
}

// launchUninstall starts the vendor uninstaller, waits for it to exit,
// then checks whether the registry entry went away. When the app is
// gone the leftover review opens automatically; when the entry is
// still there the modal opens anyway (the user may have removed files
// by hand, or the uninstaller was cancelled — the Review items say
// which is which).
func (m *Model) launchUninstall(e *uninstall.Entry) {
	if m.uninstBusy != "" || m.sweepOpen {
		return // serialized — and a review in progress owns sweepItems
	}
	cmd, err := e.Launch()
	if err != nil {
		m.uninstMsg = "Failed: " + err.Error()
		applog.Logf("uninstall launch FAILED  %s  — %v", e.Name, err)
		return
	}
	m.uninstMsg = e.Name + " — uninstaller is running…"
	m.uninstBusy = e.Name // one uninstaller at a time — MSI serializes anyway
	applog.Section("Uninstall — " + e.Name)
	applog.Logf("launched  %s", e.Uninstall)
	entry := *e
	all := append([]uninstall.Entry(nil), m.uninstRows...)
	go func() {
		_ = cmd.Wait()
		applog.Logf("uninstaller exited  %s", entry.Name)
		// Some uninstallers spawn a child and exit early — poll until
		// the registry entry disappears or a grace window elapses.
		gone := entry.Gone()
		for i := 0; i < 30 && !gone; i++ {
			time.Sleep(2 * time.Second)
			gone = entry.Gone()
		}
		// The entry vanishing does not mean file deletion finished —
		// spawned children keep working. Wait until the install
		// folder's size holds still across two samples.
		if gone && uninstall.SaneLocation(entry.Location) {
			last := uninstall.DirSize(entry.Location)
			for i := 0; i < 8; i++ {
				time.Sleep(2 * time.Second)
				cur := uninstall.DirSize(entry.Location)
				if cur == last {
					break
				}
				last = cur
			}
		}
		items := uninstall.Footprint(&entry, all)
		m.post(func() {
			m.uninstBusy = ""
			m.uninstLoaded = false // the listing changed
			m.sweepName = entry.Name
			m.sweepItems = items
			m.sweepSel = make([]bool, len(items))
			for i := range items {
				m.sweepSel[i] = true
			}
			if gone {
				// The name keeps finding leftovers later — log-driven
				// history is the orphan scan's name source.
				uninstall.RecordUninstalled(entry.Name)
			}
			switch {
			case len(items) == 0 && gone:
				m.uninstMsg = entry.Name + " uninstalled — no leftovers found"
				applog.Logf("clean uninstall  %s", entry.Name)
			case gone:
				m.uninstMsg = entry.Name + " uninstalled — reviewing leftovers"
				m.sweepOpen, m.sweepArmed = true, false
			default:
				m.uninstMsg = entry.Name + " — uninstaller exited but the entry is still listed; review before removing"
				m.sweepOpen, m.sweepArmed = true, false
			}
			if m.sweepOpen {
				uninstall.SavePending(entry.Name, items)
			}
		})
	}()
}

// sweepConfColor tags each leftover with its confidence.
func sweepConfColor(t *ui.Theme, conf uninstall.Confidence) ui.Color {
	switch conf {
	case uninstall.Confirmed:
		return t.Accent
	case uninstall.Likely:
		return t.Warning
	default:
		return t.TextMuted
	}
}

func sweepConfLabel(conf uninstall.Confidence) string {
	return [...]string{"CONFIRMED", "LIKELY", "REVIEW"}[conf]
}

// sweepModal is the leftover review list — per-item checkboxes, an
// explanation of reversibility, and the armed two-click Remove.
func (m *Model) sweepModal(c *ui.Context, t *ui.Theme) {
	ui.Modal(c, &m.sweepOpen, func() {
		ui.Column(c).Width(920).MaxHeight(680).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Text(c, "Leftovers — "+m.sweepName).Bold().FontSize(16)
			subtitle := "Everything listed belongs to " + m.sweepName + " — CONFIRMED items come from its registry record, LIKELY items match its exact name. Folders go to the Recycle Bin; registry items are exported to a .reg backup in the log folder first."
			if m.sweepName == orphanScanName {
				subtitle = "Every item is a verified leftover of something already removed — dead references, empty folders, name-matched traces. Folders go to the Recycle Bin; registry items get a .reg backup in the log folder first."
			}
			ui.Text(c, subtitle).FontSize(11).TextColor(t.TextMuted)
			if len(m.sweepItems) == 0 {
				ui.Text(c, "No leftovers found — the uninstaller left a clean machine.").FontSize(11).TextColor(t.TextMuted)
			} else {
				ui.List(c, &m.sweepList, len(m.sweepItems), func(i int) {
					it := m.sweepItems[i]
					row := ui.Row(c).FillWidth().Height(30).Gap(8).AlignItems(ui.Center).Radius(4)
					if row.Hovered() {
						row.Background(t.SurfaceHover)
					}
					row.Children(func() {
						ui.Checkbox(c, &m.sweepSel[i], "")
						ui.Row(c).Width(74).Shrink(0).Children(func() {
							ui.Text(c, sweepConfLabel(it.Confidence)).FontSize(9).Bold().TextColor(sweepConfColor(t, it.Confidence))
						})
						ui.Text(c, it.Path).FontSize(11).SingleLine().Grow(1).MinWidth(0).
							Tooltip(it.Path + "\n" + it.Why)
						if it.Kind == uninstall.KindDir && it.Bytes > 0 {
							ui.Text(c, format.Bytes(it.Bytes)).FontSize(11).Shrink(0)
						}
						ui.Text(c, it.Why).FontSize(10).TextColor(t.TextMuted).SingleLine().Shrink(0)
					})
				}).Grow(1).MinHeight(120).FillWidth()
			}
			sel, bytes := 0, uint64(0)
			for i, it := range m.sweepItems {
				if m.sweepSel[i] {
					sel++
					bytes += it.Bytes
				}
			}
			ui.Row(c).FillWidth().Height(28).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("%d of %d selected · %s", sel, len(m.sweepItems), format.Bytes(bytes))).
					FontSize(11).TextColor(t.TextMuted)
				ui.Spacer(c)
				if ui.Button(c, "Keep leftovers").Clicked() {
					uninstall.ClearPending(m.sweepName)
					m.sweepOpen = false
				}
				label := "Remove leftovers"
				if m.sweepArmed {
					label = "Sure? Files go to the Recycle Bin"
				}
				if sel == 0 {
					label = "Remove leftovers"
				}
				if m.dangerButton(c, "trash", label).Clicked() && sel > 0 {
					if !m.sweepArmed {
						m.sweepArmed = true
					} else {
						m.runSweepRemoval()
					}
				}
			})
		})
	})
}

// runSweepRemoval deletes the checked items — dirs to the Recycle Bin,
// registry items exported to a .reg backup first — and writes the whole
// operation to the applog.
func (m *Model) runSweepRemoval() {
	m.sweepArmed = false
	var chosen []uninstall.Item
	for i, it := range m.sweepItems {
		if m.sweepSel[i] {
			chosen = append(chosen, it)
		}
	}
	if len(chosen) == 0 {
		return
	}
	backup := ""
	hasReg := false
	for _, it := range chosen {
		hasReg = hasReg || it.Registry()
	}
	name := m.sweepName // capture now — the goroutine runs later
	if hasReg {
		backup = filepath.Join(applog.Dir(), fmt.Sprintf("sweep-%s-%s.reg",
			sanitizeFileName(name), time.Now().Format("20060102-150405")))
	}
	go func() {
		applog.Section("Leftover sweep — " + name)
		var reg io.Writer
		var f *os.File
		if backup != "" {
			if fh, err := os.Create(backup); err == nil {
				f, reg = fh, fh
			}
		}
		errs := uninstall.RemoveItems(chosen, reg)
		removed := 0
		var failedItems []uninstall.Item
		for i, err := range errs {
			if err == nil {
				removed++
				applog.Logf("removed  %s  (%s)", chosen[i].Path, chosen[i].Why)
			} else {
				failedItems = append(failedItems, chosen[i])
				applog.Logf("FAILED   %s  — %v", chosen[i].Path, err)
			}
		}
		if f != nil {
			f.Close()
			applog.Logf("registry backup: %s", backup)
		}
		msg := fmt.Sprintf("Removed %d leftover items", removed)
		if len(failedItems) > 0 {
			msg += fmt.Sprintf(", %d failed — see log", len(failedItems))
		}
		if backup != "" {
			msg += " · registry backed up"
		}
		applog.Logf("%s", msg)
		m.post(func() {
			if len(failedItems) > 0 {
				// Locked files and the like stay pending — the next
				// launch offers them again instead of losing them.
				uninstall.SavePending(name, failedItems)
			} else {
				uninstall.ClearPending(name)
			}
			m.sweepOpen = false
			m.uninstMsg = msg
			m.uninstLoaded = false // registry listing may be gone now
		})
	}()
}

// sanitizeFileName keeps a backup filename filesystem-safe.
func sanitizeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}
