package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/egoist/mygo/ui"
)

// junkGreen is the cleanup button's color — a broom, not the Rust gear.
var junkGreen = ui.RGB(0x1B, 0x8A, 0x5A)

// junkPreset bundles categories by risk tier, like the Rust page's
// plans. Light is every SAFE category; Deep adds the MODERATE ones.
// Windows.old stays out of both — review-only.
type junkPreset struct {
	name, tag, note string
	from, to        int // inclusive category range
}

var junkPresets = []junkPreset{
	{name: "Light", tag: "SAFE", note: "temp, caches and reports — everything regenerates or re-downloads",
		from: 0, to: disk.JunkCatRecycleBin},
	{name: "Deep", tag: "MODERATE", note: "Light plus update staging, crash dumps, logs and dev caches",
		from: 0, to: disk.JunkCatDevCaches},
}

// junkScan measures every cleanup location off the UI goroutine. The
// report is self-contained — junk lives in fixed locations, so no disk
// index is needed or built.
func (m *Model) junkScan() {
	if m.junkBusy {
		return
	}
	m.junkBusy = true
	m.junkAnCat = 0
	go func() {
		var running map[string]bool
		if infos, err := m.processSvc.Snapshot(); err == nil {
			running = make(map[string]bool, len(infos))
			for _, in := range infos {
				running[strings.ToLower(in.Name)] = true
			}
		}
		rep := disk.FindJunk(disk.JunkOpts{
			Running: func(name string) bool {
				return running != nil && running[name]
			},
			Progress: func(cat int) {
				m.post(func() { m.junkAnCat = cat })
			},
		})
		m.post(func() {
			m.junkBusy = false
			m.junkReport = rep
			m.junkMode = true
			m.rustMode = false
			m.junkSel, m.junkConfirm = -1, -1
			m.junkRowConfirm = ""
			m.junkFilterCat = -1
			m.junkPathsCat, m.junkPathsOpen = -1, false
			if m.junkDoneMsg != "" {
				m.junkMsg, m.junkDoneMsg = m.junkDoneMsg, ""
			}
		})
	}()
}

// junkPresetJobs collects every cleanable path in a preset's category
// range — rows blocked by a running app stay out.
func (m *Model) junkPresetJobs(p int) []*disk.JunkDir {
	rep := m.junkReport
	if rep == nil || p < 0 || p >= len(junkPresets) {
		return nil
	}
	var jobs []*disk.JunkDir
	for cat := junkPresets[p].from; cat <= junkPresets[p].to; cat++ {
		for i := range rep.Cats[cat].Dirs {
			d := &rep.Cats[cat].Dirs[i]
			if d.Skip == "" {
				jobs = append(jobs, d)
			}
		}
	}
	return jobs
}

func junkJobsBytes(jobs []*disk.JunkDir) uint64 {
	var b uint64
	for _, j := range jobs {
		b += j.Bytes
	}
	return b
}

// junkRow is one row in the cleanup table — a group header (head set)
// or one measured location (d set).
type junkRow struct {
	head string
	cat  int
	d    *disk.JunkDir
	risk int
}

// junkRows builds the display list: category filter, then size sort,
// then optional risk-level grouping with header rows.
func (m *Model) junkRows() []junkRow {
	rep := m.junkReport
	var rows []junkRow
	for cat := 0; cat < disk.JunkCatCount; cat++ {
		if m.junkFilterCat >= 0 && m.junkFilterCat != cat {
			continue
		}
		for i := range rep.Cats[cat].Dirs {
			rows = append(rows, junkRow{cat: cat, d: &rep.Cats[cat].Dirs[i], risk: rep.Cats[cat].Risk})
		}
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].d.Bytes > rows[b].d.Bytes })
	if !m.junkGroupSafe {
		return rows
	}
	grouped := make([]junkRow, 0, len(rows)+4)
	for risk := disk.RustRiskSafe; risk <= disk.RustRiskReview; risk++ {
		var n int
		var bytes uint64
		for _, r := range rows {
			if r.risk == risk {
				n++
				bytes += r.d.Bytes
			}
		}
		if n == 0 {
			continue
		}
		label := "items"
		if n == 1 {
			label = "item"
		}
		grouped = append(grouped, junkRow{head: fmt.Sprintf("%s — %d %s · %s",
			rustRiskLabel(risk), n, label, format.Bytes(bytes)), cat: -1, risk: risk})
		for _, r := range rows {
			if r.risk == risk {
				grouped = append(grouped, r)
			}
		}
	}
	return grouped
}

// junkDelete removes the jobs off the UI goroutine, then re-measures so
// the report reflects what was actually freed — locked files stay.
func (m *Model) junkDelete(jobs []*disk.JunkDir) {
	if len(jobs) == 0 {
		return
	}
	m.junkBusy = true
	m.junkMsg = "Cleaning…"
	go func() {
		var freed uint64
		var removed, skipped, failed int
		var firstErr error
		for _, d := range jobs {
			f, rm, sk, err := disk.JunkClean(*d)
			freed += f
			removed += rm
			skipped += sk
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		m.post(func() {
			m.junkBusy = false
			m.junkConfirm, m.junkSel = -1, -1
			m.junkRowConfirm = ""
			msg := fmt.Sprintf("Freed %s", format.Bytes(freed))
			if removed > 0 {
				msg += fmt.Sprintf(" · %s removed", format.Int(uint64(removed)))
			}
			if skipped > 0 {
				msg += fmt.Sprintf(" · %s in use, skipped", format.Int(uint64(skipped)))
			}
			if failed > 0 && firstErr != nil {
				msg += fmt.Sprintf(" · %d failed: %s", failed, firstErr)
			}
			m.junkDoneMsg = msg
			m.junkScan()
		})
	}()
}

// junkButton is the toolbar's cleanup trigger: white broom on green.
func (m *Model) junkButton(c *ui.Context, off bool) ui.Element {
	e := ui.Row(c).Size(34, 28).Radius(6).Center().Tooltip("Clean everyday junk — temp, caches, dumps").Label("Junk cleanup")
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		fill := junkGreen
		switch {
		case e.Pressed():
			fill = ui.RGB(0x11, 0x6B, 0x42)
		case e.Hovered():
			fill = ui.RGB(0x16, 0x7D, 0x4E)
		}
		if off {
			fill = fill.Alpha(0.45)
		}
		p.Fill(r, fill, 6)
	})
	e.Children(func() { icon(c, "broom", 16).TextColor(ui.RGB(255, 255, 255)) })
	return e.Disabled(off)
}

// junkAnalyzingView is the placeholder while locations are measured —
// the per-category progress callback feeds the live counter.
func (m *Model) junkAnalyzingView(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		card := ui.Column(c).Width(440).Padding(16, 20).Gap(10).Background(t.Surface).Radius(8)
		card.BorderWidth(1).BorderColor(t.Border)
		card.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				icon(c, "broom", 18)
				ui.Text(c, "Measuring cleanup locations").FontSize(14).Bold()
			})
			cat := m.junkAnCat
			if cat >= disk.JunkCatCount {
				cat = disk.JunkCatCount - 1
			}
			ui.Text(c, fmt.Sprintf("%d of %d · %s…", cat+1, disk.JunkCatCount, disk.JunkCatName(cat))).
				FontSize(11).TextColor(t.TextMuted).SingleLine()
			ui.Progress(c, float64(cat+1)/float64(disk.JunkCatCount)).FillWidth().Height(4)
		})
		ui.Spacer(c)
	})
}

// junkResultsView fills the disk page with the cleanup report: preset
// plans up top, then the unified table — every measured location is a
// row, filterable by category badge and groupable by risk.
func (m *Model) junkResultsView(c *ui.Context, t *ui.Theme) {
	rep := m.junkReport
	ui.Column(c).Fill().Padding(4, 12).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Height(30).AlignItems(ui.Center).Gap(8).Children(func() {
			icon(c, "broom", 18)
			ui.Text(c, fmt.Sprintf("Cleanup — %s reclaimable", format.Bytes(rep.Bytes))).Bold().Shrink(0)
			if m.junkBusy {
				ui.Text(c, "working…").FontSize(11).TextColor(t.TextMuted)
			} else if m.junkMsg != "" {
				ui.Text(c, m.junkMsg).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			}
			ui.Spacer(c)
			ui.Text(c, "Fixed, known-safe locations only — nothing is searched for or guessed.").FontSize(11).TextColor(t.TextMuted).SingleLine()
			if m.iconButton(c, "x", "Back to disk view").Clicked() {
				m.junkMode = false
			}
		})
		rustSectionLabel(c, t, "Pick a plan — or click one to preview what it removes")
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			for p := range junkPresets {
				m.junkPresetCard(c, t, p)
			}
			// REVIEW note card: what never enters a plan.
			if len(rep.Cats[disk.JunkCatWindowsOld].Dirs) > 0 {
				ui.Column(c).Grow(1).Padding(10, 12).Gap(4).Background(t.Surface).Radius(8).Children(func() {
					ui.Text(c, "Not in any plan").FontSize(10).Bold().TextColor(t.TextMuted)
					ui.Text(c, "Windows.old is listed below — deleting it removes the rollback option.").
						FontSize(11).TextColor(t.TextMuted)
				}).BorderWidth(1).BorderColor(t.Border)
			}
		})
		m.junkFilterStrip(c, t)
		rows := m.junkRows()
		if len(rows) == 0 {
			msg := "Nothing to clean — every location is empty."
			if m.junkFilterCat >= 0 {
				msg = "No rows match this filter."
			}
			ui.Text(c, msg).TextColor(t.TextMuted)
		} else {
			rustSectionLabel(c, t, "Items — every measured location, one row each")
			ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(236).Shrink(0)
				ui.Row(c).Width(78).Shrink(0).Children(func() {
					ui.Text(c, "Risk").FontSize(11).Bold().TextColor(t.TextMuted)
				})
				ui.Text(c, "Location").FontSize(11).Bold().TextColor(t.TextMuted).Grow(1).MinWidth(120).SingleLine()
				ui.Row(c).Width(64).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, "Files").FontSize(11).Bold().TextColor(t.TextMuted)
				})
				ui.Row(c).Width(92).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, "Size").FontSize(11).Bold().TextColor(t.TextMuted)
				})
				ui.Box(c).Width(76).Shrink(0)
			})
			ui.List(c, &m.junkList, len(rows), func(i int) {
				r := rows[i]
				if r.head != "" {
					ui.Row(c).FillWidth().Height(26).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, r.head).FontSize(11).Bold().TextColor(rustRiskColor(t, r.risk)).Shrink(0)
						ui.Text(c, "· "+rustRiskNote(r.risk)).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
					})
					return
				}
				hit := false
				if m.junkSel >= 0 && m.junkSel < len(junkPresets) && r.d.Skip == "" {
					hit = r.cat >= junkPresets[m.junkSel].from && r.cat <= junkPresets[m.junkSel].to
				}
				row := ui.Row(c).FillWidth().Height(28).Padding(0, 10).Gap(6).AlignItems(ui.Center).Radius(4)
				hovered := row.Hovered()
				switch {
				case hit:
					row.Background(t.Accent.Alpha(0.12))
				case hovered:
					row.Background(t.SurfaceHover)
				}
				row.Children(func() {
					ui.Row(c).Width(236).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
						for cat := 0; cat < disk.JunkCatCount; cat++ {
							dim := cat != r.cat
							if m.junkFilterCat >= 0 {
								dim = cat != m.junkFilterCat
							}
							tierBadge(c, t, cat+1, dim).Tooltip(rep.Cats[cat].Name)
						}
					})
					ui.Row(c).Width(78).Shrink(0).Children(func() {
						ui.Text(c, rustRiskLabel(r.risk)).FontSize(10).Bold().TextColor(rustRiskColor(t, r.risk))
					})
					ui.Text(c, rep.Cats[r.cat].Name).FontSize(11).TextColor(t.Accent).Shrink(0)
					ptxt := ui.Text(c, r.d.Path).SingleLine().Tooltip(r.d.Path).Grow(1).MinWidth(0)
					if r.d.Skip != "" {
						ptxt.TextColor(t.TextMuted)
					}
					files := format.Int(uint64(r.d.Files))
					if r.d.Files < 0 {
						files = "—"
					}
					ui.Row(c).Width(64).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, files).SingleLine()
					})
					ui.Row(c).Width(92).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, format.Bytes(r.d.Bytes)).SingleLine()
					})
					ui.Row(c).Width(76).Shrink(0).Justify(ui.End).Children(func() {
						m.junkRowButton(c, t, r)
					})
				})
			}).Grow(1).FillWidth()
		}
	})
	m.junkCatPathsModal(c, t)
}

// junkFilterStrip is the category filter row — one clickable badge per
// category, the group-by-safety toggle at the end.
func (m *Model) junkFilterStrip(c *ui.Context, t *ui.Theme) {
	rep := m.junkReport
	ui.Row(c).FillWidth().Height(26).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Filter:").FontSize(11).TextColor(t.TextMuted).Shrink(0)
		for cat := 0; cat < disk.JunkCatCount; cat++ {
			cat := cat
			empty := len(rep.Cats[cat].Dirs) == 0
			dim := empty || (m.junkFilterCat >= 0 && m.junkFilterCat != cat)
			b := tierBadge(c, t, cat+1, dim).Tooltip(rep.Cats[cat].Name + " — " + rep.Cats[cat].Note).Cursor(ui.CursorPointer)
			if b.Clicked() && !empty {
				if m.junkFilterCat == cat {
					m.junkFilterCat = -1
				} else {
					m.junkFilterCat = cat
				}
			}
		}
		ui.Checkbox(c, &m.junkGroupSafe, "Group by safety")
		if m.junkFilterCat >= 0 {
			cat := m.junkFilterCat
			ui.Text(c, "· "+rep.Cats[cat].Name).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			m.junkPathsLink(c, t, cat)
			if m.iconButton(c, "x", "Clear filter").Clicked() {
				m.junkFilterCat = -1
			}
		}
		ui.Spacer(c)
	})
}

// junkPresetCard is one recommended plan — badges show the category
// range it bundles; Clean arms a second-click confirmation.
func (m *Model) junkPresetCard(c *ui.Context, t *ui.Theme, p int) {
	pre := junkPresets[p]
	jobs := m.junkPresetJobs(p)
	bytes := junkJobsBytes(jobs)
	on := m.junkSel == p
	card := ui.Column(c).Grow(1).Padding(10, 12).Gap(6).Background(t.Surface).Radius(8)
	card.BorderWidth(1).BorderColor(t.Border).Cursor(ui.CursorPointer)
	if on {
		card.BorderColor(t.Accent)
	}
	card.Tooltip("Click to highlight the files this plan removes")
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, pre.name).Bold().FontSize(13)
			if p == 0 {
				ui.Text(c, "recommended").FontSize(10).TextColor(t.Accent)
			}
			ui.Spacer(c)
			ui.Text(c, pre.tag).FontSize(10).Bold().TextColor(rustRiskColor(t, p))
		})
		ui.Text(c, format.Bytes(bytes)).FontSize(18).Bold()
		ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
			for cat := pre.from; cat <= pre.to; cat++ {
				tierBadge(c, t, cat+1, len(m.junkReport.Cats[cat].Dirs) == 0).Tooltip(m.junkReport.Cats[cat].Name)
			}
		})
		ui.Text(c, pre.note).FontSize(10).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip(pre.note)
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if m.dangerButton(c, "broom", "Clean").Disabled(bytes == 0 || m.junkBusy).Clicked() {
				m.junkConfirm = p
			}
		})
		if m.junkConfirm == p {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, fmt.Sprintf("Delete %d locations, free %s?", len(jobs), format.Bytes(bytes))).
					FontSize(11).TextColor(t.Danger).Grow(1).MinWidth(0)
				if ui.Button(c, "Confirm").Clicked() {
					m.junkDelete(jobs)
				}
				if ui.Button(c, "Keep").Clicked() {
					m.junkConfirm = -1
				}
			})
		}
	})
	if card.Clicked() {
		if on {
			m.junkSel = -1
		} else {
			m.junkSel = p
		}
	}
}

// junkPathsLink opens the category's path-list modal — the "check what
// you are about to delete" step.
func (m *Model) junkPathsLink(c *ui.Context, t *ui.Theme, i int) {
	cat := &m.junkReport.Cats[i]
	e := ui.Row(c).Shrink(0).Gap(3).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label("Show paths in " + cat.Name)
	e.Children(func() {
		label := "paths"
		if len(cat.Dirs) == 1 {
			label = "path"
		}
		ui.Text(c, fmt.Sprintf("%s %s", format.Int(uint64(len(cat.Dirs))), label)).FontSize(11).TextColor(t.Accent)
	})
	if e.Hovered() {
		e.Background(t.SurfaceHover)
	}
	if e.Clicked() {
		m.junkPathsCat = i
		m.junkPathsOpen = true
	}
}

// junkCatPathsModal lists every path inside a category.
func (m *Model) junkCatPathsModal(c *ui.Context, t *ui.Theme) {
	if m.junkPathsCat < 0 || m.junkPathsCat >= disk.JunkCatCount || m.junkReport == nil {
		m.junkPathsOpen = false
		return
	}
	cat := &m.junkReport.Cats[m.junkPathsCat]
	ui.Modal(c, &m.junkPathsOpen, func() {
		ui.Column(c).Width(600).MaxHeight(430).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Text(c, cat.Name).Bold().FontSize(16)
			ui.Text(c, cat.Note).FontSize(11).TextColor(t.TextMuted)
			ui.List(c, &m.junkPathsList, len(cat.Dirs), func(i int) {
				d := cat.Dirs[i]
				ui.Row(c).FillWidth().Height(24).Gap(8).AlignItems(ui.Center).Children(func() {
					g := "folder"
					if d.File {
						g = "file"
					}
					icon(c, g, 13).TextColor(t.TextMuted)
					ui.Text(c, d.Path).FontSize(11).SingleLine().Grow(1).MinWidth(0).Tooltip(d.Path)
					ui.Text(c, format.Bytes(d.Bytes)).FontSize(11).TextColor(t.TextMuted).Shrink(0)
				})
			}).Grow(1).MinHeight(0).FillWidth()
		})
	})
}

// junkRowButton is the per-row clean control: first click arms, second
// deletes. Rows blocked by a running app show the reason instead.
func (m *Model) junkRowButton(c *ui.Context, t *ui.Theme, r junkRow) {
	if r.d.Skip != "" {
		ui.Text(c, r.d.Skip).FontSize(10).TextColor(t.Warning).SingleLine().Tooltip("Close the app, then re-scan to clean it")
		return
	}
	path := r.d.Path
	armed := m.junkRowConfirm == path
	label, tip := "Clean", "Delete "+path
	if r.d.Mode == disk.JunkShell {
		label, tip = "Empty", "Empty the Recycle Bin on all drives"
	}
	if armed {
		label, tip = "Sure?", "Click again to delete "+path
	}
	e := ui.Row(c).Height(22).PaddingX(8).Gap(4).AlignItems(ui.Center).Radius(t.Radius).Label("Clean " + path).Tooltip(tip)
	e.BorderWidth(1).BorderColor(t.Border)
	if armed {
		e.Background(t.Danger.Alpha(0.12)).BorderColor(t.Danger)
	} else {
		e.Background(t.Surface)
	}
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if e.Pressed() {
			p.Fill(r, t.SurfacePressed, t.Radius)
		} else if e.Hovered() {
			p.Fill(r, t.SurfaceHover, t.Radius)
		}
	})
	e.Children(func() {
		icon(c, "trash", 12).TextColor(t.Danger)
		ui.Text(c, label).FontSize(11).SingleLine().TextColor(t.Danger)
	})
	if e.Disabled(m.junkBusy).Clicked() {
		if armed {
			m.junkDelete([]*disk.JunkDir{r.d})
		} else {
			m.junkRowConfirm = path
		}
	}
}
