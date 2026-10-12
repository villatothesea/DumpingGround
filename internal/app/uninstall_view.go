package app

import (
	"context"
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
				// Re-validate: the app may have been reinstalled in the
				// meantime, and saved paths may have been cleaned by
				// hand. Stale pending data must never preselect live
				// files.
				if name != orphanScanName && uninstall.StillInstalled(name, m.uninstRows) {
					uninstall.ClearPending(name)
					m.uninstMsg = name + " is installed again — its saved leftover review was dropped"
				} else {
					var kept []uninstall.Item
					for _, it := range items {
						if it.Registry() || it.Kind != uninstall.KindDir || dirExists(it.Path) {
							kept = append(kept, it)
						}
					}
					if len(kept) == 0 {
						uninstall.ClearPending(name)
						m.uninstMsg = name + " — saved leftovers are already gone"
					} else {
						items = kept
						m.sweepName, m.sweepItems = name, items
						m.sweepSel = make([]bool, len(items))
						for i := range items {
							m.sweepSel[i] = items[i].Confidence == uninstall.Confirmed
						}
						m.sweepOpen, m.sweepArmed = true, false
						m.uninstMsg = name + " — leftover review resumed from before the app was closed"
					}
				}
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
				sel := row == m.uninstSelected
				text, muted := t.Text, t.TextMuted
				if sel { // the row paints accent — light text or it is unreadable
					text, muted = t.AccentText, t.AccentText
				}
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
							icon(c, "pkg", 13).TextColor(muted)
						}
						ui.Text(c, e.Name).SingleLine().TextColor(text).Tooltip(e.Name + "  " + e.Version)
					case 1:
						ui.Text(c, e.Version).SingleLine().TextColor(text).Tooltip(e.Version)
					case 2:
						ui.Text(c, e.Publisher).TextColor(muted).SingleLine().Tooltip(e.Publisher)
					case 3:
						sz := "—"
						if e.EstimatedSize {
							sz = format.Bytes(e.SizeBytes)
						}
						ui.Text(c, sz).TextColor(text)
					case 4:
						d := e.Date()
						if d == "" {
							d = "—"
						}
						tip := e.InstallDate
						if e.DateEstimated {
							tip += " (estimated — the key's last write, not the install day)"
						}
						ui.Text(c, d).TextColor(muted).Tooltip(tip)
					case 5:
						ui.Text(c, e.Location).TextColor(muted).SingleLine().Tooltip(e.Location)
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
		op := ui.Row(c).Height(24).PaddingX(7).Gap(4).AlignItems(ui.Center).Radius(t.Radius).
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
		u := ui.Row(c).Height(24).PaddingX(8).Gap(4).AlignItems(ui.Center).Radius(t.Radius).
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
			ui.Text(c, label).TextColor(t.Danger).SingleLine()
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

// The review dialog's phases — the modal opens the moment a scan
// starts and walks through them instead of appearing with results.
const (
	sweepReview   = iota // the checkbox list (also the zero value)
	sweepScanning        // progress bar while a scan runs
	sweepRemoving        // progress bar while items delete
	sweepDone            // confirm-close alert with the result
)

// openSweepScan puts the leftover dialog up in its scanning phase.
func (m *Model) openSweepScan(name string) {
	m.sweepName, m.sweepPhase, m.sweepArmed = name, sweepScanning, false
	m.sweepScanText, m.sweepScanFrac = "Starting the scan…", 0
	m.sweepItems = nil
	m.sweepLiveApp = false
	m.sweepOpen = true
}

// scanProgress is the Footprint/Orphans callback: each stage posts its
// label and 0→1 fraction to the dialog.
func (m *Model) scanProgress(base float64) func(string, float64) {
	return func(stage string, f float64) {
		m.postProg(func() {
			m.sweepScanText = stage
			m.sweepScanFrac = base + (1-base)*f
		})
	}
}

// finishSweepScan presents what a scan produced — the checkbox list,
// or the "clean machine" alert when empty. Only Confirmed items start
// checked: a Likely row is a name-matched guess and must be opted into
// by hand.
func (m *Model) finishSweepScan(name string, items []uninstall.Item) {
	m.sweepItems = items
	m.sweepSel = make([]bool, len(items))
	for i := range items {
		m.sweepSel[i] = items[i].Confidence == uninstall.Confirmed
	}
	if len(items) > 0 {
		uninstall.SavePending(name, items)
	}
	m.sweepPhase = sweepReview
}

// startSweep scans an entry's leftovers without uninstalling it — the
// "Find leftovers" menu action. Same review modal as the post-uninstall
// flow. When the app is still installed nothing Confirmed can be shown:
// its install folder, its uninstall key and its startup entries are
// live, so the listing degrades to unchecked Likely matches only.
func (m *Model) startSweep(e *uninstall.Entry) {
	m.openSweepScan(e.Name)
	all := append([]uninstall.Entry(nil), m.uninstRows...)
	entry := *e
	go func() {
		items := uninstall.Footprint(&entry, all, m.scanProgress(0))
		m.post(func() {
			if !entry.Gone() {
				m.sweepLiveApp = true
				var kept []uninstall.Item
				for _, it := range items {
					if it.Confidence != uninstall.Confirmed {
						kept = append(kept, it)
					}
				}
				items = kept
			}
			m.finishSweepScan(entry.Name, items)
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
	m.openSweepScan(orphanScanName)
	all := append([]uninstall.Entry(nil), m.uninstRows...)
	go func() {
		items := uninstall.Orphans(all, m.scanProgress(0))
		applog.Logf("orphan scan: %d items", len(items))
		m.post(func() {
			m.orphanScanning = false
			if len(items) == 0 {
				m.uninstMsg = "No leftovers of removed apps found"
			}
			m.finishSweepScan(orphanScanName, items)
		})
	}()
}

// launchUninstall starts the vendor uninstaller, waits for it to exit,
// then checks whether the registry entry went away. Only a gone entry
// means a real uninstall — a surviving entry means the user cancelled
// or the uninstaller bailed, and sweeping then would list the live
// install folder as a leftover.
func (m *Model) launchUninstall(e *uninstall.Entry) {
	if (m.uninstBusy != "" && m.uninstBusy != e.Name) || m.sweepOpen {
		return // serialized — but a cancelled uninstall may be retried at once
	}
	if m.uninstCancel != nil {
		// A retry for the same app retires the watch that is still
		// polling for the last attempt — two watchers would both fire
		// the sweep dialog and trample each other's results.
		m.uninstCancel()
		m.uninstCancel = nil
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
	// The record lands now, not when the entry vanishes: an uninstall
	// that finishes after our watch (or after this app closed) still
	// leaves a name the next orphan scan looks for.
	uninstall.RecordLaunch(e)
	entry := *e
	all := append([]uninstall.Entry(nil), m.uninstRows...)
	ctx, cancel := context.WithCancel(m.ctx)
	m.uninstCancel = cancel
	go func() {
		_ = cmd.Wait()
		applog.Logf("uninstaller exited  %s", entry.Name)
		m.post(func() { m.uninstMsg = entry.Name + " — watching for the uninstaller to finish" })
		// Forking uninstallers (Inno) exit their parent while the real
		// uninstaller window still asks for confirmation — the registry
		// entry is the only truth. Watch it silently for five minutes;
		// nothing pops up while the user thinks, and a surviving entry
		// means cancelled / never ran — treated as if nothing happened.
		gone := entry.Gone()
		for i := 0; i < 150 && !gone && ctx.Err() == nil; i++ {
			time.Sleep(2 * time.Second)
			gone = entry.Gone()
		}
		if ctx.Err() != nil {
			return // a retry retired this watch — stay quiet
		}
		if !gone {
			applog.Logf("uninstaller exited with entry still listed  %s — treated as cancelled", entry.Name)
			m.post(func() {
				m.uninstBusy = ""
				m.uninstMsg = entry.Name + " — uninstaller closed, entry still listed; nothing changed"
			})
			return
		}
		// The entry is gone — a real uninstall. NOW the dialog opens:
		// children may still be deleting files, so the scan's settle
		// wait is shown rather than hidden.
		m.post(func() {
			m.openSweepScan(entry.Name)
			m.sweepScanText = "Waiting for file cleanup to settle…"
			m.sweepScanFrac = 0.1
		})
		if uninstall.SaneLocation(entry.Location) {
			last := uninstall.DirSize(entry.Location)
			for i := 0; i < 8; i++ {
				time.Sleep(2 * time.Second)
				frac := 0.1 + 0.3*float64(i)/8
				m.postProg(func() { m.sweepScanFrac = frac })
				cur := uninstall.DirSize(entry.Location)
				if cur == last {
					break
				}
				last = cur
			}
		}
		items := uninstall.Footprint(&entry, all, m.scanProgress(0.4))
		m.post(func() {
			m.uninstBusy = ""
			m.uninstLoaded = false // the listing changed
			m.sweepName = entry.Name
			// The name keeps finding leftovers later — log-driven
			// history is the orphan scan's name source.
			uninstall.RecordUninstalled(&entry)
			if len(items) == 0 {
				m.uninstMsg = entry.Name + " uninstalled — no leftovers found"
				applog.Logf("clean uninstall  %s", entry.Name)
			} else {
				m.uninstMsg = entry.Name + " uninstalled — reviewing leftovers"
			}
			m.finishSweepScan(entry.Name, items)
		})
	}()
}

// sweepModal is the leftover review list — per-item checkboxes, an
// explanation of reversibility, and the armed two-click Remove.
func (m *Model) sweepModal(c *ui.Context, t *ui.Theme) {
	switch m.sweepPhase {
	case sweepScanning:
		ui.Modal(c, &m.sweepOpen, func() {
			ui.Column(c).Width(440).Padding(6, 16, 14, 16).Gap(10).Children(func() {
				ui.Text(c, "Leftovers — "+m.sweepName).Bold().FontSize(16)
				ui.Text(c, m.sweepScanText).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
				ui.Progress(c, m.sweepScanFrac).FillWidth()
			})
		})
		return
	case sweepRemoving:
		ui.Modal(c, &m.sweepOpen, func() {
			ui.Column(c).Width(440).Padding(6, 16, 14, 16).Gap(10).Children(func() {
				ui.Text(c, "Removing leftovers — "+m.sweepName).Bold().FontSize(16)
				ui.Text(c, fmt.Sprintf("%d of %d · %s", m.sweepRmDone, m.sweepRmTotal, m.sweepRmPath)).
					FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
				ui.Progress(c, float64(m.sweepRmDone)/float64(max(m.sweepRmTotal, 1))).FillWidth()
			})
		})
		return
	case sweepDone:
		if ui.AlertDialog(c, &m.sweepOpen, "Leftovers — "+m.sweepName, m.sweepDoneMsg, "OK") >= 0 {
			m.sweepPhase = sweepReview
		}
		return
	}
	if len(m.sweepItems) == 0 {
		// Nothing to review — a one-button alert, not the full list UI.
		msg := "No leftovers found."
		if m.sweepName == orphanScanName {
			msg = "No leftovers found — no dead references or empty folders."
		}
		if ui.AlertDialog(c, &m.sweepOpen, "Leftovers — "+m.sweepName, msg, "OK") >= 0 {
			uninstall.ClearPending(m.sweepName)
			m.sweepArmed = false
		}
		return
	}
	ui.Modal(c, &m.sweepOpen, func() {
		// Height follows the item count — every leftover is visible at
		// once — until fifteen rows, then the list scrolls rather than
		// towering over the window. The virtual List only renders rows
		// its laid-out height shows, so the column carries the size.
		rows := len(m.sweepItems)
		if rows > 15 {
			rows = 15
		}
		ui.Column(c).MaxHeight(900).MinHeight(float32(rows)*30+132).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Text(c, "Leftovers — "+m.sweepName).Bold().FontSize(16)
			subtitle := "Everything listed belongs to " + m.sweepName + " — verified against its registry record or its exact name. Folders go to the Recycle Bin; registry items are exported to a .reg backup in the log folder first."
			if m.sweepName == orphanScanName {
				subtitle = "Every item is a verified leftover of something already removed — dead references, empty folders, name-matched traces. Folders go to the Recycle Bin; registry items get a .reg backup in the log folder first."
			}
			if m.sweepLiveApp {
				subtitle = m.sweepName + " is still installed — its install folder, app entry and startup items are live and never listed here. What remains are name-matched folders only; none is preselected."
			}
			ui.Text(c, subtitle).FontSize(11).TextColor(t.TextMuted).MaxWidth(760)
			ui.List(c, &m.sweepList, len(m.sweepItems), func(i int) {
				it := m.sweepItems[i]
				row := ui.Row(c).FillWidth().Height(30).Gap(8).AlignItems(ui.Center).Radius(4)
				if row.Hovered() {
					row.Background(t.SurfaceHover)
				}
				row.Children(func() {
					if ui.Checkbox(c, &m.sweepSel[i], "").Changed() {
						// A changed selection voids the armed confirm —
						// the second click must cover what is ticked.
						m.sweepArmed = false
					}
					ui.Text(c, it.Path).FontSize(13).SingleLine().Grow(1).MinWidth(0).
						Tooltip(it.Path + "\n" + it.Why)
					if it.Kind == uninstall.KindDir && it.Bytes > 0 {
						ui.Text(c, format.Bytes(it.Bytes)).FontSize(13).Shrink(0)
					}
					ui.Text(c, it.Why).FontSize(13).TextColor(t.TextMuted).SingleLine().Shrink(0)
				})
			}).Grow(1).FillWidth().Padding(0, 12, 0, 0)
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
				if m.iconTextButton(c, "right", "Keep leftovers").Clicked() {
					uninstall.ClearPending(m.sweepName)
					m.sweepOpen = false
				}
				label := "Remove leftovers"
				if m.sweepArmed {
					label = fmt.Sprintf("Sure? Remove %d to the Recycle Bin", sel)
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
	m.sweepPhase = sweepRemoving
	m.sweepRmDone, m.sweepRmTotal, m.sweepRmPath = 0, len(chosen), ""
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
			} else {
				applog.Logf("registry backup FAILED to create  %s  — %v", backup, err)
			}
		}
		errs := uninstall.RemoveItems(chosen, reg, func(done, total int, path string) {
			m.postProg(func() {
				m.sweepRmDone, m.sweepRmTotal, m.sweepRmPath = done, total, path
			})
		})
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
		regBackedUp := false
		if f != nil {
			// Sync before Close — a crash between now and the flush
			// must not leave an empty .reg standing for deleted keys.
			regBackedUp = f.Sync() == nil && f.Close() == nil
			if regBackedUp {
				applog.Logf("registry backup: %s", backup)
			} else {
				_ = f.Close()
				applog.Logf("registry backup FAILED to write  %s", backup)
			}
		}
		msg := fmt.Sprintf("Removed %d leftover items", removed)
		if len(failedItems) > 0 {
			msg += fmt.Sprintf(", %d failed — see log", len(failedItems))
		}
		if regBackedUp {
			msg += " · registry backed up"
		}
		applog.Logf("%s", msg)
		m.post(func() {
			if len(failedItems) > 0 {
				// Locked files and the like stay pending — the next
				// launch offers them again instead of losing them.
				uninstall.SavePending(name, failedItems)
				msg += "\nFailed items will be offered again next launch."
			} else {
				uninstall.ClearPending(name)
			}
			m.sweepDoneMsg = msg
			m.sweepPhase = sweepDone
			m.uninstMsg = strings.SplitN(msg, "\n", 2)[0]
			m.uninstLoaded = false // registry listing may be gone now
		})
	}()
}

// dirExists reports whether a saved leftover path still exists — used
// when a pending review is restored days later.
func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
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
