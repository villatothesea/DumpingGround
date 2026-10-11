package app

import (
	"fmt"
	"os/exec"
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
	name, tag, note, cost string
	from, to              int // inclusive category range the plan removes
}

var junkPresets = []junkPreset{
	{name: "Light", tag: "SAFE", note: "all eight SAFE categories — temp, caches, reports",
		cost: "Cost: nothing — everything regenerates or re-downloads on its own",
		from: 0, to: disk.JunkCatAppCaches},
	{name: "Deep", tag: "+ MODERATE", note: "everything in Light, plus update staging, dumps, logs, dev caches",
		cost: "Cost: re-downloads, one slower update, lost diagnostic logs",
		from: 0, to: disk.JunkCatDevCaches},
}

// Grouping dimensions for the junk table. Both can be on at once —
// the table nests two levels, ordered by junkCatFirst.
const (
	junkDimRisk = iota
	junkDimCat
)

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

// junkRow is one row in the cleanup table — a group header (head set;
// headDim is the dimension it groups by) or one measured location
// (d set). depth is the indent level for grouped rows and headers.
type junkRow struct {
	head    string
	headDim int    // junkDimRisk / junkDimCat for headers, -1 for data rows
	key     string // collapse key for headers
	depth   int
	cat     int
	d       *disk.JunkDir
	risk    int
}

// junkRows builds the display list: category filter, then size sort,
// then grouping by the enabled dimensions — risk and category may nest,
// outer first per junkCatFirst. Collapsed groups keep their header.
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
	var dims []int
	if m.junkCatFirst {
		if m.junkGroupCat {
			dims = append(dims, junkDimCat)
		}
		if m.junkGroupRisk {
			dims = append(dims, junkDimRisk)
		}
	} else {
		if m.junkGroupRisk {
			dims = append(dims, junkDimRisk)
		}
		if m.junkGroupCat {
			dims = append(dims, junkDimCat)
		}
	}
	if len(dims) == 0 {
		return rows
	}
	var grouped []junkRow
	var emit func(members []junkRow, depth, dimIdx int, prefix string)
	emit = func(members []junkRow, depth, dimIdx int, prefix string) {
		if dimIdx == len(dims) {
			for _, r := range members {
				r.depth = depth
				grouped = append(grouped, r)
			}
			return
		}
		dim := dims[dimIdx]
		dimChar := "r"
		n := disk.RustRiskReview + 1
		if dim == junkDimCat {
			dimChar, n = "c", disk.JunkCatCount
		}
		for v := 0; v < n; v++ {
			var sub []junkRow
			var bytes uint64
			for _, r := range members {
				key := r.risk
				if dim == junkDimCat {
					key = r.cat
				}
				if key == v {
					sub = append(sub, r)
					bytes += r.d.Bytes
				}
			}
			if len(sub) == 0 {
				continue
			}
			var head string
			headRisk := disk.RustRiskSafe
			if dim == junkDimCat {
				head = fmt.Sprintf("%s — %s · %s", rep.Cats[v].Name, junkItemCount(len(sub)), format.Bytes(bytes))
				headRisk = rep.Cats[v].Risk
			} else {
				head = fmt.Sprintf("%s — %s · %s", rustRiskLabel(v), junkItemCount(len(sub)), format.Bytes(bytes))
				headRisk = v
			}
			key := prefix + dimChar + fmt.Sprint(v)
			cat := -1
			if dim == junkDimCat {
				cat = v
			}
			grouped = append(grouped, junkRow{head: head, headDim: dim, key: key, depth: depth, cat: cat, risk: headRisk})
			if m.junkCollapsed[key] {
				continue
			}
			emit(sub, depth+1, dimIdx+1, key+"/")
		}
	}
	emit(rows, 0, 0, "")
	return grouped
}

func junkItemCount(n int) string {
	if n == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", n)
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
					ui.Text(c, "Windows.old and upgrade leftovers — deleting removes the rollback option.").
						FontSize(11).TextColor(t.TextMuted)
				}).BorderWidth(1).BorderColor(t.Border)
			}
		})
		// Downloads is the user's call, not ours — measured for a
		// reminder card with an Open shortcut, never part of a plan.
		if rep.Downloads != nil {
			ui.Row(c).FillWidth().Gap(10).Children(func() {
				m.junkDownloadsCard(c, t, rep.Downloads)
			})
		}
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
				ui.Box(c).Width(260).Shrink(0)
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
					collapsed := m.junkCollapsed[r.key]
					g := "caret-d"
					if collapsed {
						g = "caret-r"
					}
					he := ui.Row(c).FillWidth().Height(26).Padding(0, float32(10+r.depth*24)).Gap(6).AlignItems(ui.Center).Radius(4).
						Cursor(ui.CursorPointer).Label("Toggle group " + r.head)
					if he.Hovered() {
						he.Background(t.SurfaceHover)
					}
					he.Children(func() {
						icon(c, g, 10).TextColor(t.TextMuted)
						if r.headDim == junkDimCat {
							tierBadge(c, t, r.cat+1, false)
							ui.Text(c, r.head).FontSize(11).Bold().TextColor(t.Text).Shrink(0)
							ui.Text(c, "· "+rep.Cats[r.cat].Note).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
						} else {
							ui.Text(c, r.head).FontSize(11).Bold().TextColor(rustRiskColor(t, r.risk)).Shrink(0)
							ui.Text(c, "· "+rustRiskNote(r.risk)).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
						}
					})
					if he.Clicked() {
						m.junkCollapsed[r.key] = !collapsed
					}
					return
				}
				hit := false
				if m.junkSel >= 0 && m.junkSel < len(junkPresets) && r.d.Skip == "" {
					hit = r.cat >= junkPresets[m.junkSel].from && r.cat <= junkPresets[m.junkSel].to
				}
				row := ui.Row(c).FillWidth().Height(28).Padding(0, float32(10+r.depth*24)).Gap(6).AlignItems(ui.Center).Radius(4)
				hovered := row.Hovered()
				switch {
				case hit:
					row.Background(t.Accent.Alpha(0.12))
				case hovered:
					row.Background(t.SurfaceHover)
				}
				row.Children(func() {
					ui.Row(c).Width(260).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
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
					note := rep.Cats[r.cat].Note
					if r.d.Warn != "" {
						note = r.d.Warn
					}
					// Every row carries a consequence hint — the marker's
					// shape and color encode how much it matters.
					switch {
					case r.risk == disk.RustRiskSafe:
						icon(c, "info", 12).TextColor(t.TextMuted).Tooltip(note)
					case r.risk == disk.RustRiskReview:
						icon(c, "warn", 12).TextColor(t.Danger).Tooltip(note)
					default:
						icon(c, "warn", 12).TextColor(t.Warning).Tooltip(note)
					}
					ui.Text(c, rep.Cats[r.cat].Name).FontSize(11).TextColor(t.Accent).Shrink(0).
						Tooltip(note)
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
	m.junkLegendModal(c, t)
}

// junkLegendModal explains the numbered badges: every category with
// its badge, name, consequence note and risk level.
func (m *Model) junkLegendModal(c *ui.Context, t *ui.Theme) {
	rep := m.junkReport
	ui.Modal(c, &m.junkLegendOpen, func() {
		ui.Column(c).Width(640).MaxHeight(520).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Text(c, "What the numbers mean").Bold().FontSize(16)
			ui.Text(c, "Each number is a cleanup category. Every row lights the badge of the category it belongs to; the filter badges above narrow the table to one category.").
				FontSize(11).TextColor(t.TextMuted)
			ui.List(c, &m.junkLegendList, disk.JunkCatCount, func(i int) {
				cat := &rep.Cats[i]
				ui.Row(c).FillWidth().Height(26).Gap(10).AlignItems(ui.Center).Children(func() {
					tierBadge(c, t, i+1, len(cat.Dirs) == 0)
					ui.Row(c).Width(78).Shrink(0).Children(func() {
						ui.Text(c, rustRiskLabel(cat.Risk)).FontSize(10).Bold().TextColor(rustRiskColor(t, cat.Risk))
					})
					ui.Text(c, cat.Name).FontSize(11).Bold().Shrink(0).Width(130)
					ui.Text(c, cat.Note).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0).Tooltip(cat.Note)
				})
			}).Height(390).FillWidth()
		})
	})
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
		q := ui.Row(c).Size(16, 16).Shrink(0).Radius(8).BorderWidth(1).BorderColor(t.TextMuted.Alpha(0.5)).
			Center().Cursor(ui.CursorPointer).Tooltip("What do the numbers mean?").Label("Category legend")
		q.Children(func() {
			ui.Text(c, "?").FontSize(10).Bold().TextColor(t.TextMuted)
		})
		if q.Hovered() {
			q.Background(t.SurfaceHover)
		}
		if q.Clicked() {
			m.junkLegendOpen = true
		}
		ui.Text(c, "Group:").FontSize(11).TextColor(t.TextMuted).Shrink(0)
		ui.Checkbox(c, &m.junkGroupRisk, "Risk").Tooltip("Group rows under risk-level headers")
		ui.Checkbox(c, &m.junkGroupCat, "Category").Tooltip("Group rows under category headers")
		if m.junkGroupRisk && m.junkGroupCat {
			order := "Risk → Category"
			if m.junkCatFirst {
				order = "Category → Risk"
			}
			if ui.Button(c, "⇄ "+order).Height(20).Clicked() {
				m.junkCatFirst = !m.junkCatFirst
			}
		}
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
		ui.Text(c, pre.cost).FontSize(10).TextColor(t.Warning).SingleLine().Ellipsis("…").Tooltip(pre.cost)
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

// junkDownloadsCard is the reminder strip under the plans: Downloads
// holds user files, so we only measure it, offer an Open shortcut to
// look, and a confirmed Clean for those who want it gone anyway.
func (m *Model) junkDownloadsCard(c *ui.Context, t *ui.Theme, dl *disk.JunkDir) {
	card := ui.Row(c).FillWidth().Padding(8, 14).Gap(12).AlignItems(ui.Center).Background(t.Surface).Radius(8)
	card.BorderWidth(1).BorderColor(t.Border)
	card.Children(func() {
		icon(c, "folder", 16).TextColor(t.Warning)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, fmt.Sprintf("Downloads — %s · %s files", format.Bytes(dl.Bytes), format.Int(uint64(dl.Files)))).
				FontSize(12).Bold().SingleLine()
			ui.Text(c, "Your files — no plan touches this. Look and decide yourself.").
				FontSize(10).TextColor(t.TextMuted).SingleLine().MinWidth(0)
		})
		op := ui.Row(c).Height(24).PaddingX(10).Gap(5).AlignItems(ui.Center).Radius(t.Radius).
			Cursor(ui.CursorPointer).Label("Open Downloads").Tooltip("Open in Explorer")
		op.BorderWidth(1).BorderColor(t.Border).Background(t.Surface)
		op.Draw(func(p *ui.Painter, r ui.Rect) {
			if op.Pressed() {
				p.Fill(r, t.SurfacePressed, t.Radius)
			} else if op.Hovered() {
				p.Fill(r, t.SurfaceHover, t.Radius)
			}
		})
		op.Children(func() {
			icon(c, "folder", 12).TextColor(t.Accent)
			ui.Text(c, "Open").FontSize(11).TextColor(t.Accent)
		})
		if op.Clicked() {
			_ = exec.Command("explorer", dl.Path).Start()
		}
		armed := m.junkRowConfirm == dl.Path
		label := "Clean"
		if armed {
			label = "Sure?"
		}
		cl := ui.Row(c).Height(24).PaddingX(10).Gap(5).AlignItems(ui.Center).Radius(t.Radius).
			Cursor(ui.CursorPointer).Label("Clean Downloads").
			Tooltip("Delete everything inside Downloads — the folder itself stays")
		cl.BorderWidth(1).BorderColor(t.Border).Background(t.Surface)
		if armed {
			cl.Background(t.Danger.Alpha(0.12)).BorderColor(t.Danger)
		}
		cl.Draw(func(p *ui.Painter, r ui.Rect) {
			if cl.Pressed() {
				p.Fill(r, t.SurfacePressed, t.Radius)
			} else if cl.Hovered() {
				p.Fill(r, t.SurfaceHover, t.Radius)
			}
		})
		cl.Children(func() {
			icon(c, "trash", 12).TextColor(t.Danger)
			ui.Text(c, label).FontSize(11).TextColor(t.Danger)
		})
		if cl.Disabled(m.junkBusy).Clicked() {
			if armed {
				m.junkDelete([]*disk.JunkDir{dl})
			} else {
				m.junkRowConfirm = dl.Path
			}
		}
	})
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
					tip := d.Path
					if d.Warn != "" {
						tip += " — " + d.Warn
					}
					ui.Text(c, d.Path).FontSize(11).SingleLine().Grow(1).MinWidth(0).Tooltip(tip)
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
