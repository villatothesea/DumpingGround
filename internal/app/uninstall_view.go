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
			ui.Text(c, "Uninstall launches the app's own uninstaller — nothing is deleted directly.").FontSize(11).TextColor(t.TextMuted).SingleLine()
			if m.iconButton(c, "refresh", "Re-read the installed-apps list").Clicked() {
				m.uninstLoaded = false
			}
		})
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
		m.uninstHeader(c, t)
		m.sweepModal(c, t)
		ui.List(c, &m.uninstList, len(rows), func(i int) {
			e := rows[i]
			row := ui.Row(c).FillWidth().Height(28).Padding(0, 10).Gap(8).AlignItems(ui.Center).Radius(4)
			if row.Hovered() {
				row.Background(t.SurfaceHover)
			}
			row.Children(func() {
				icon(c, "pkg", 13).TextColor(t.TextMuted)
				name := ui.Text(c, e.Name).SingleLine().Grow(1).MinWidth(0)
				name.Tooltip(e.Name + "  " + e.Version)
				if e.Version != "" {
					ui.Text(c, e.Version).FontSize(11).TextColor(t.TextMuted).Shrink(0).MaxWidth(90).SingleLine().Ellipsis("…")
				}
				ui.Row(c).Width(180).Shrink(0).Children(func() {
					ui.Text(c, e.Publisher).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip(e.Publisher)
				})
				ui.Row(c).Width(76).Shrink(0).Justify(ui.End).Children(func() {
					sz := "—"
					if e.EstimatedSize {
						sz = format.Bytes(e.SizeBytes)
					}
					ui.Text(c, sz).FontSize(11)
				})
				ui.Row(c).Width(86).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, e.Date()).FontSize(11).TextColor(t.TextMuted)
				})
				m.uninstRowActions(c, t, e)
			})
		}).Grow(1).MinHeight(0).FillWidth()
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
		switch m.uninstSortCol {
		case 0:
			lt = strings.ToLower(ra.Name) < strings.ToLower(rb.Name)
		case 1:
			lt = strings.ToLower(ra.Publisher) < strings.ToLower(rb.Publisher)
		case 2:
			lt = ra.SizeBytes < rb.SizeBytes
		default:
			lt = ra.InstallDate < rb.InstallDate
		}
		if m.uninstSortDesc {
			return !lt
		}
		return lt
	}
	sort.SliceStable(out, less)
	return out
}

// uninstHeader draws the sortable column header row.
func (m *Model) uninstHeader(c *ui.Context, t *ui.Theme) {
	ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(13).Shrink(0)
		ui.Row(c).Grow(1).MinWidth(0).Children(func() {
			m.uninstColHead(c, t, 0, "Name", 0)
		})
		ui.Row(c).Width(180).Shrink(0).Children(func() {
			m.uninstColHead(c, t, 1, "Publisher", 0)
		})
		ui.Row(c).Width(76).Shrink(0).Justify(ui.End).Children(func() {
			m.uninstColHead(c, t, 2, "Size", 0)
		})
		ui.Row(c).Width(86).Shrink(0).Justify(ui.End).Children(func() {
			m.uninstColHead(c, t, 3, "Installed", 0)
		})
		ui.Box(c).Width(178).Shrink(0)
	})
}

// uninstColHead is one clickable column title — click flips the sort.
func (m *Model) uninstColHead(c *ui.Context, t *ui.Theme, col int, label string, w float32) {
	e := ui.Row(c).Gap(3).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label("Sort by " + label)
	e.Children(func() {
		ui.Text(c, label).FontSize(11).Bold().TextColor(t.TextMuted)
		if m.uninstSortCol == col {
			g := "chup"
			if m.uninstSortDesc {
				g = "caret-d"
			}
			icon(c, g, 9).TextColor(t.Accent)
		}
	})
	if e.Clicked() {
		if m.uninstSortCol == col {
			m.uninstSortDesc = !m.uninstSortDesc
		} else {
			m.uninstSortCol, m.uninstSortDesc = col, col == 2
		}
	}
}

// uninstRowActions are the per-row buttons: open the install folder,
// and an armed two-click Uninstall that launches the app's own
// uninstaller (which brings its own confirmation UI).
func (m *Model) uninstRowActions(c *ui.Context, t *ui.Theme, e *uninstall.Entry) {
	ui.Row(c).Width(178).Shrink(0).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
		swp := ui.Row(c).Height(22).PaddingX(7).Gap(4).AlignItems(ui.Center).Radius(t.Radius).
			Cursor(ui.CursorPointer).Label("Find leftovers").Tooltip(
			"Scan for files, registry keys and startup entries this app would leave behind")
		swp.BorderWidth(1).BorderColor(t.Border).Background(t.Surface)
		swp.Draw(func(p *ui.Painter, r ui.Rect) {
			if swp.Hovered() {
				p.Fill(r, t.SurfaceHover, t.Radius)
			}
		})
		swp.Children(func() {
			icon(c, "search", 11).TextColor(t.Accent)
			ui.Text(c, "Sweep").FontSize(11)
		})
		if swp.Clicked() {
			m.startSweep(e)
		}
		if e.Location != "" {
			op := ui.Row(c).Height(22).PaddingX(7).Gap(4).AlignItems(ui.Center).Radius(t.Radius).
				Cursor(ui.CursorPointer).Label("Open install folder").Tooltip(e.Location)
			op.BorderWidth(1).BorderColor(t.Border).Background(t.Surface)
			op.Draw(func(p *ui.Painter, r ui.Rect) {
				if op.Hovered() {
					p.Fill(r, t.SurfaceHover, t.Radius)
				}
			})
			op.Children(func() {
				icon(c, "folder", 11).TextColor(t.Accent)
			})
			if op.Clicked() {
				_ = exec.Command("explorer", e.Location).Start()
			}
		}
		armed := m.uninstArmed == e.Name
		label := "Uninstall"
		if armed {
			label = "Sure?"
		}
		u := ui.Row(c).Height(22).PaddingX(8).Gap(4).AlignItems(ui.Center).Radius(t.Radius).
			Cursor(ui.CursorPointer).Label("Uninstall "+e.Name).
			Tooltip("Launch the app's own uninstaller: "+e.Uninstall)
		u.BorderWidth(1).BorderColor(t.Border).Background(t.Surface)
		if armed {
			u.Background(t.Danger.Alpha(0.12)).BorderColor(t.Danger)
		}
		u.Draw(func(p *ui.Painter, r ui.Rect) {
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
		if u.Clicked() {
			if !armed {
				m.uninstArmed = e.Name
				return
			}
			m.uninstArmed = ""
			if err := e.Launch(); err != nil {
				m.uninstMsg = "Failed: " + err.Error()
				applog.Logf("uninstall launch FAILED  %s  — %v", e.Name, err)
			} else {
				m.uninstMsg = "Uninstaller launched for " + e.Name + " — refresh when it finishes"
				applog.Logf("uninstall launched  %s  (%s)", e.Name, e.Uninstall)
			}
		}
	})
}

// startSweep scans one entry's leftovers off the UI goroutine, then
// opens the review modal. Confirmed and Likely items come pre-checked;
// Review items need an explicit tick.
func (m *Model) startSweep(e *uninstall.Entry) {
	if m.sweepBusy {
		return
	}
	m.sweepBusy, m.sweepName, m.sweepArmed = true, e.Name, false
	go func() {
		items := uninstall.Footprint(e, m.uninstRows)
		m.post(func() {
			m.sweepBusy = false
			m.sweepItems = items
			m.sweepSel = make([]bool, len(items))
			for i := range items {
				m.sweepSel[i] = items[i].Confidence != uninstall.Review
			}
			m.sweepOpen = true
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
		ui.Column(c).Width(780).MaxHeight(560).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Text(c, "Leftovers — "+m.sweepName).Bold().FontSize(16)
			ui.Text(c, "Folders and shortcuts go to the Recycle Bin. Registry items are exported to a .reg backup in the log folder before removal. REVIEW rows are unchecked — tick them only after looking.").
				FontSize(11).TextColor(t.TextMuted)
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
						ui.Text(c, it.Why).FontSize(10).TextColor(t.TextMuted).SingleLine().Shrink(0).MaxWidth(220).Ellipsis("…")
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
	if hasReg {
		backup = filepath.Join(applog.Dir(), fmt.Sprintf("sweep-%s-%s.reg",
			sanitizeFileName(m.sweepName), time.Now().Format("20060102-150405")))
	}
	go func() {
		applog.Section("Leftover sweep — " + m.sweepName)
		var reg io.Writer
		var f *os.File
		if backup != "" {
			if fh, err := os.Create(backup); err == nil {
				f, reg = fh, fh
			}
		}
		errs := uninstall.RemoveItems(chosen, reg)
		removed, failed := 0, 0
		for i, err := range errs {
			if err == nil {
				removed++
				applog.Logf("removed  %s  (%s)", chosen[i].Path, chosen[i].Why)
			} else {
				failed++
				applog.Logf("FAILED   %s  — %v", chosen[i].Path, err)
			}
		}
		if f != nil {
			f.Close()
			applog.Logf("registry backup: %s", backup)
		}
		msg := fmt.Sprintf("Removed %d leftover items", removed)
		if failed > 0 {
			msg += fmt.Sprintf(", %d failed — see log", failed)
		}
		if backup != "" {
			msg += " · registry backed up"
		}
		applog.Logf("%s", msg)
		m.post(func() {
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
