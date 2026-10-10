package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/egoist/mygo/ui"
)

// rustOrange is the Rust brand color; the toolbar button is icon-only,
// white glyph on orange.
var rustOrange = ui.RGB(0xF7, 0x4C, 0x00)

// rustPreset bundles cleanup categories into one recommended action, so
// the user picks a risk level instead of nine separate switches.
// allTargets means "every project target dir whole" — the nuke option.
type rustPreset struct {
	name, tag, includes, rebuild string
	cats                         []int
	allTargets                   bool
}

var rustPresets = []rustPreset{
	{name: "Light", tag: "SAFE", includes: "Incremental · docs · cargo caches", rebuild: "Rebuild: nothing — Cargo re-downloads", cats: []int{disk.RustCatIncr, disk.RustCatDoc, disk.RustCatRegistry, disk.RustCatGit}},
	{name: "Deep", tag: "MODERATE", includes: "Light + debug builds", rebuild: "Rebuild: next cargo build", cats: []int{disk.RustCatIncr, disk.RustCatDoc, disk.RustCatDebug, disk.RustCatRegistry, disk.RustCatGit}},
	{name: "Nuke", tag: "AGGRESSIVE", includes: "All targets + cargo caches", rebuild: "Rebuild: everything incl. release", allTargets: true, cats: []int{disk.RustCatRegistry, disk.RustCatGit}},
}

// rustScan analyzes the current snapshot for Rust build artifacts. When
// the index is missing or belongs to another path — the user picked a
// different drive after scanning — it scans the selected path first and
// analyzes once that settles; an index built only for this is released
// again right after, since the report is self-contained.
func (m *Model) rustScan() {
	if m.diskSnapshot == nil || m.indexIsStale() {
		m.rustPending = true
		if m.diskCancel == nil {
			m.startScan()
			m.rustOwnsIndex = m.diskCancel != nil
		}
		return
	}
	m.rustAnalyze()
}

// rustAnalyze runs the artifact detection off the UI goroutine — the walk
// touches every node of a possibly multi-million-node index.
func (m *Model) rustAnalyze() {
	s := m.diskSnapshot
	if s == nil || m.rustBusy || m.indexIsStale() {
		return
	}
	m.rustBusy = true
	m.rustMsg = ""
	gone := m.rustGoneFn()
	go func() {
		rep := disk.FindRustTargets(s, gone)
		m.post(func() {
			m.rustBusy = false
			m.rustReport = rep
			m.rustMode = true
			m.rustSel = -1
			m.rustConfirm = -1
			m.rustRowConfirm = ""
			m.rustAdvConfirm = false
			m.rustChecked = [disk.RustCatCount]bool{}
			m.rustPathsCat = -1
			m.rustPathsOpen = false
			if m.rustOwnsIndex {
				m.rustOwnsIndex = false
				m.releaseIndexForRust()
			}
		})
	}()
}

// rustGoneFn builds the path predicate that marks directories deleted in
// this session, so re-analysis does not resurface them before the index
// catches up through the USN journal.
func (m *Model) rustGoneFn() func(string) bool {
	if len(m.rustGone) == 0 {
		return nil
	}
	roots := make([]string, 0, len(m.rustGone))
	for p := range m.rustGone {
		roots = append(roots, p+string(filepath.Separator))
	}
	return func(path string) bool {
		if m.rustGone[path] {
			return true
		}
		for _, r := range roots {
			if strings.HasPrefix(path, r) {
				return true
			}
		}
		return false
	}
}

// rustPresetJobs collects the deduplicated paths a preset deletes: its
// categories' dirs, plus every project target when it is the nuke.
func (m *Model) rustPresetJobs(p int) []disk.RustDir {
	rep := m.rustReport
	if rep == nil || p < 0 || p >= len(rustPresets) {
		return nil
	}
	pre := rustPresets[p]
	var jobs []disk.RustDir
	if pre.allTargets {
		for _, it := range rep.Items {
			jobs = append(jobs, disk.RustDir{Path: it.Path, Bytes: it.Total, Base: "target"})
		}
	}
	for _, c := range pre.cats {
		jobs = append(jobs, rep.Cats[c].Dirs...)
	}
	return disk.DedupRustDirs(jobs)
}

// rustCheckedJobs collects the deduplicated paths of every category the
// user ticked in the advanced panel.
func (m *Model) rustCheckedJobs() []disk.RustDir {
	rep := m.rustReport
	if rep == nil {
		return nil
	}
	var jobs []disk.RustDir
	for c := range rep.Cats {
		if m.rustChecked[c] {
			jobs = append(jobs, rep.Cats[c].Dirs...)
		}
	}
	return disk.DedupRustDirs(jobs)
}

func rustJobsBytes(jobs []disk.RustDir) uint64 {
	var b uint64
	for _, j := range jobs {
		b += j.Bytes
	}
	return b
}

// rustCatCovers reports whether a cleanup category touches a project row —
// shared caches and orphans touch no row.
func rustCatCovers(it disk.RustItem, cat int) bool {
	switch cat {
	case disk.RustCatIncr:
		return it.Incr > 0
	case disk.RustCatDoc:
		return it.Doc > 0
	case disk.RustCatDebug:
		return it.Debug > 0
	case disk.RustCatRelease:
		return it.Rel > 0
	case disk.RustCatStray:
		return it.Stray > 0
	}
	return false
}

// rustRowHit reports whether the current selection — a preset (0-2) or an
// advanced category (100+i) — removes parts of the item's target dir.
func rustRowHit(it disk.RustItem, sel int) bool {
	switch {
	case sel < 0:
		return false
	case sel >= 100:
		return rustCatCovers(it, sel-100)
	default:
		pre := rustPresets[sel]
		if pre.allTargets {
			return true
		}
		for _, c := range pre.cats {
			if c <= disk.RustCatStray && rustCatCovers(it, c) {
				return true
			}
		}
		return false
	}
}

// cleanRustPreset removes every path in the preset after the per-path
// re-check inside rustDelete. The report is self-contained, so this works
// with or without an index.
func (m *Model) cleanRustPreset(p int) {
	if m.rustBusy {
		return
	}
	m.rustDelete(m.rustPresetJobs(p))
}

// cleanRustItem removes one project's whole target/ dir — the row-level
// version of the nuke.
func (m *Model) cleanRustItem(i int) {
	rep := m.rustReport
	if rep == nil || m.rustBusy || i < 0 || i >= len(rep.Items) {
		return
	}
	it := rep.Items[i]
	m.rustDelete([]disk.RustDir{{Path: it.Path, Bytes: it.Total, Base: "target"}})
}

// cleanRustChecked removes the union of every ticked advanced category.
func (m *Model) cleanRustChecked() {
	if m.rustBusy {
		return
	}
	m.rustDelete(m.rustCheckedJobs())
}

// rustDelete runs the actual removals off the UI goroutine, then folds the
// outcome back into the report. Every path is revalidated just before it
// goes — the report is seconds stale and may describe a path that has
// since been renamed or replaced.
func (m *Model) rustDelete(jobs []disk.RustDir) {
	jobs = disk.DedupRustDirs(jobs)
	if len(jobs) == 0 {
		return
	}
	m.rustBusy = true
	m.rustMsg = "Deleting…"
	recycle := m.rustRecycle
	go func() {
		var freed uint64
		var failed, unbinned int
		var firstErr error
		deleted := map[string]uint64{}
		for _, j := range jobs {
			if err := disk.SafeRustEntry(j.Path, j.Base, j.File); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			var err error
			if recycle {
				err = disk.RecycleFileOrDir(j.Path)
				if err != nil {
					// Some paths cannot be binned — too big, or the
					// shell refused. Remove them directly and say so.
					unbinned++
					err = os.RemoveAll(j.Path)
				}
			} else {
				err = os.RemoveAll(j.Path)
			}
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			deleted[j.Path] = j.Bytes
			freed += j.Bytes
			m.post(func() { m.rustGone[j.Path] = true })
		}
		m.post(func() {
			m.rustBusy = false
			m.rustConfirm = -1
			m.rustRowConfirm = ""
			m.rustAdvConfirm = false
			m.rustChecked = [disk.RustCatCount]bool{}
			m.rustMsg = fmt.Sprintf("Deleted %s items, freed about %s", format.Int(uint64(len(deleted))), format.Bytes(freed))
			if unbinned > 0 {
				m.rustMsg += fmt.Sprintf(" · %s could not be recycled and were removed", format.Int(uint64(unbinned)))
			}
			if failed > 0 && firstErr != nil {
				m.rustMsg += fmt.Sprintf(" · %s failed: %s", format.Int(uint64(failed)), firstErr)
			}
			if m.rustReport != nil {
				m.rustReport.ApplyDeleted(deleted)
			}
			if m.diskSnapshot != nil {
				m.rustAnalyze()
			}
		})
	}()
}

// rustButton is the toolbar's Rust-artifact scan trigger: icon-only, white
// glyph on the Rust brand orange.
func (m *Model) rustButton(c *ui.Context, off bool) ui.Element {
	e := ui.Row(c).Size(34, 28).Radius(6).Center().Tooltip("Find Cargo target/ dirs and clean them by tier").Label("Rust scan")
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		fill := rustOrange
		switch {
		case e.Pressed():
			fill = ui.RGB(0xC4, 0x3A, 0x00)
		case e.Hovered():
			fill = ui.RGB(0xE6, 0x47, 0x00)
		}
		if off {
			fill = fill.Alpha(0.45)
		}
		p.Fill(r, fill, 6)
	})
	e.Children(func() { icon(c, "rust", 16).TextColor(ui.RGB(255, 255, 255)) })
	return e.Disabled(off)
}

// rustAnalyzingView is the brief placeholder between a finished scan and
// the completed artifact analysis.
func (m *Model) rustAnalyzingView(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			icon(c, "rust", 22)
			ui.Text(c, "Analyzing Rust build artifacts…").TextColor(t.TextMuted)
		})
		ui.Spacer(c)
	})
}

// tierBadge is the numbered plan marker: a rounded square in the theme
// accent with a white digit, dimmed when the plan does not apply.
func tierBadge(c *ui.Context, t *ui.Theme, n int, dim bool) ui.Element {
	bg, fg := t.Accent, t.AccentText
	if dim {
		bg, fg = t.TextMuted.Alpha(0.25), t.TextMuted
	}
	return ui.Row(c).Size(16, 16).Shrink(0).Radius(4).Background(bg).Center().Children(func() {
		ui.Text(c, fmt.Sprint(n)).FontSize(10).Bold().TextColor(fg)
	})
}

// rustRiskColor maps a category risk level to a theme color.
func rustRiskColor(t *ui.Theme, risk int) ui.Color {
	switch risk {
	case disk.RustRiskSafe:
		return t.Success
	case disk.RustRiskModerate:
		return t.Warning
	case disk.RustRiskCareful:
		return t.Danger
	}
	return t.TextMuted
}

func rustRiskLabel(risk int) string {
	return []string{"SAFE", "MODERATE", "CAREFUL", "REVIEW"}[risk]
}

// rustConfirmText is the arm prompt for a delete batch — it says "Recycle"
// while the bin toggle is on so the verb matches what will happen.
func (m *Model) rustConfirmText(jobs []disk.RustDir) string {
	n, b := len(jobs), rustJobsBytes(jobs)
	if m.rustRecycle {
		return fmt.Sprintf("Recycle %s items (%s)?", format.Int(uint64(n)), format.Bytes(b))
	}
	return fmt.Sprintf("Delete %s items, free %s?", format.Int(uint64(n)), format.Bytes(b))
}

// rustResultsView fills the disk page with the artifact report: three
// preset plans on top, a collapsible advanced panel with all nine
// categories, then every project target dir in the shared table.
func (m *Model) rustResultsView(c *ui.Context, t *ui.Theme) {
	rep := m.rustReport
	ui.Column(c).Fill().Padding(4, 12).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Height(30).AlignItems(ui.Center).Gap(8).Children(func() {
			icon(c, "rust", 18)
			ui.Text(c, fmt.Sprintf("Rust build artifacts · %s projects", format.Int(uint64(rep.Projects)))).Bold().Shrink(0)
			if m.rustBusy {
				ui.Text(c, "working…").FontSize(11).TextColor(t.TextMuted)
			} else if m.rustMsg != "" {
				ui.Text(c, m.rustMsg).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			}
			ui.Spacer(c)
			ui.Checkbox(c, &m.rustRecycle, "Recycle Bin").Tooltip("Send deletions to the Recycle Bin where possible")
			sortLabel := "Sort: size"
			if m.rustSortIdle {
				sortLabel = "Sort: idle"
			}
			if ui.Button(c, sortLabel).Clicked() {
				m.rustSortIdle = !m.rustSortIdle
				m.rustSortItems()
			}
			if m.iconButton(c, "x", "Back to disk view").Clicked() {
				m.rustMode = false
			}
		})
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			for p := range rustPresets {
				m.rustPresetCard(c, t, p)
			}
		})
		m.rustAdvancedPanel(c, t)
		if rep.Projects == 0 {
			ui.Text(c, "No Cargo target/ directories in this index.").TextColor(t.TextMuted)
		} else {
			// Header matching the row layout below.
			ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(100).Shrink(0)
				ui.Text(c, "Project").FontSize(11).Bold().TextColor(t.TextMuted).Grow(1).MinWidth(180).SingleLine()
				for _, h := range []string{"Incr", "Debug", "Release", "Total"} {
					ui.Row(c).Width(100).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, h).FontSize(11).Bold().TextColor(t.TextMuted)
					})
				}
				ui.Row(c).Width(64).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, "%").FontSize(11).Bold().TextColor(t.TextMuted)
				})
				ui.Row(c).Width(56).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, "Idle").FontSize(11).Bold().TextColor(t.TextMuted)
				})
				ui.Box(c).Width(76).Shrink(0)
			})
			total := max(rep.Scanned, 1)
			ui.List(c, &m.rustList, len(rep.Items), func(i int) {
				it := rep.Items[i]
				hit := m.rustSel >= 0 && rustRowHit(it, m.rustSel)
				row := ui.Row(c).FillWidth().Height(28).Padding(0, 10).Gap(6).AlignItems(ui.Center).Radius(4)
				hovered := row.Hovered()
				switch {
				case hit:
					row.Background(t.Accent.Alpha(0.12))
				case hovered:
					row.Background(t.SurfaceHover)
				}
				row.Children(func() {
					ui.Row(c).Width(100).Shrink(0).Gap(4).Children(func() {
						for cat := disk.RustCatIncr; cat <= disk.RustCatStray; cat++ {
							dim := !rustCatCovers(it, cat)
							if m.rustSel >= 0 {
								if m.rustSel >= 100 {
									dim = dim || m.rustSel-100 != cat
								} else {
									dim = dim || !hit
								}
							}
							tierBadge(c, t, cat+1, dim)
						}
					})
					path := filepath.Dir(it.Path)
					ptxt := ui.Text(c, path).SingleLine().Tooltip(path).Grow(1).MinWidth(0)
					if it.Idle > 60 {
						ptxt.TextColor(t.TextMuted)
					}
					for _, v := range []uint64{it.Incr, it.Debug, it.Rel, it.Total} {
						ui.Row(c).Width(100).Shrink(0).Justify(ui.End).Children(func() {
							ui.Text(c, format.Bytes(v)).SingleLine()
						})
					}
					ui.Row(c).Width(64).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, fmt.Sprintf("%.1f%%", float64(it.Total)/float64(total)*100)).SingleLine()
					})
					ui.Row(c).Width(56).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, rustIdleText(it.Idle)).SingleLine().TextColor(t.TextMuted)
					})
					ui.Row(c).Width(76).Shrink(0).Justify(ui.End).Children(func() {
						m.rustRowButton(c, t, i, it)
					})
				})
			}).Grow(1).FillWidth()
		}
	})
	m.rustCatPathsModal(c, t)
}

// rustPresetCard is one recommended plan: its numbered badge, risk tag,
// what it bundles, reclaimable size, rebuild cost and a Clean button that
// asks for a second click. Clicking the card selects the plan and lights
// up the rows it removes.
func (m *Model) rustPresetCard(c *ui.Context, t *ui.Theme, p int) {
	pre := rustPresets[p]
	jobs := m.rustPresetJobs(p)
	bytes := rustJobsBytes(jobs)
	on := m.rustSel == p
	tagColor := rustRiskColor(t, p)
	card := ui.Column(c).Grow(1).Padding(10, 12).Gap(6).Background(t.Surface).Radius(8)
	card.BorderWidth(1).BorderColor(t.Border).Cursor(ui.CursorPointer)
	if on {
		card.BorderColor(t.Accent)
	}
	card.Tooltip("Click to highlight the files this plan removes")
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			tierBadge(c, t, p+1, false)
			ui.Text(c, pre.name).Bold().FontSize(13)
			if p == 0 {
				ui.Text(c, "recommended").FontSize(10).TextColor(t.Accent)
			}
			ui.Spacer(c)
			ui.Text(c, pre.tag).FontSize(10).Bold().TextColor(tagColor)
		})
		ui.Text(c, pre.includes).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, format.Bytes(bytes)).FontSize(16).Bold()
				ui.Text(c, pre.rebuild).FontSize(10).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip(pre.rebuild)
			})
			if m.dangerButton(c, "trash", "Clean").Disabled(bytes == 0 || m.rustBusy).Clicked() {
				m.rustConfirm = p
			}
		})
		if m.rustConfirm == p {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, m.rustConfirmText(jobs)).FontSize(11).TextColor(t.Danger).Grow(1).MinWidth(0)
				if ui.Button(c, "Confirm").Clicked() {
					m.cleanRustPreset(p)
				}
				if ui.Button(c, "Keep").Clicked() {
					m.rustConfirm = -1
				}
			})
		}
	})
	if card.Clicked() {
		if on {
			m.rustSel = -1
		} else {
			m.rustSel = p
		}
	}
}

// rustAdvancedPanel is the collapsible category picker: every one of the
// nine cleanup categories with its own checkbox, risk tag, path count and
// size — the precise tool for users who do not want a preset.
func (m *Model) rustAdvancedPanel(c *ui.Context, t *ui.Theme) {
	rep := m.rustReport
	ui.Collapsible(c, "Choose exactly what to remove", &m.rustAdvanced, func() {
		ui.Column(c).Gap(2).PaddingY(4).Children(func() {
			for i := range rep.Cats {
				cat := &rep.Cats[i]
				row := ui.Row(c).FillWidth().Height(26).Padding(0, 10).Gap(8).AlignItems(ui.Center).Radius(4).Cursor(ui.CursorPointer)
				if row.Hovered() {
					row.Background(t.SurfaceHover)
				}
				i := i
				row.Children(func() {
					ui.CheckboxBase(c, &m.rustChecked[i])
					tierBadge(c, t, i+1, false)
					ui.Text(c, cat.Name).FontSize(12).Shrink(0).Width(130)
					ui.Text(c, cat.Note).FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(cat.Note)
					ui.Text(c, rustRiskLabel(cat.Risk)).FontSize(10).Bold().TextColor(rustRiskColor(t, cat.Risk)).Shrink(0)
					if len(cat.Dirs) > 0 {
						if ui.Button(c, fmt.Sprintf("%s paths", format.Int(uint64(len(cat.Dirs))))).Clicked() {
							m.rustPathsCat = i
							m.rustPathsOpen = true
						}
					}
					ui.Row(c).Width(80).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, format.Bytes(cat.Bytes)).FontSize(12).SingleLine()
					})
				})
				if row.Clicked() {
					m.rustChecked[i] = !m.rustChecked[i]
					if m.rustChecked[i] {
						m.rustSel = 100 + i
					} else if m.rustSel == 100+i {
						m.rustSel = -1
					}
				}
			}
			ui.Row(c).FillWidth().Height(30).Padding(0, 10).Gap(8).AlignItems(ui.Center).Children(func() {
				jobs := m.rustCheckedJobs()
				ui.Text(c, fmt.Sprintf("%s items · %s", format.Int(uint64(len(jobs))), format.Bytes(rustJobsBytes(jobs)))).FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0)
				if m.dangerButton(c, "trash", "Clean selected").Disabled(len(jobs) == 0 || m.rustBusy).Clicked() {
					m.rustAdvConfirm = true
				}
			})
			if m.rustAdvConfirm {
				jobs := m.rustCheckedJobs()
				ui.Row(c).FillWidth().Height(30).Padding(0, 10).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, m.rustConfirmText(jobs)).FontSize(11).TextColor(t.Danger).Grow(1).MinWidth(0)
					if ui.Button(c, "Confirm").Clicked() {
						m.cleanRustChecked()
					}
					if ui.Button(c, "Keep").Clicked() {
						m.rustAdvConfirm = false
					}
				})
			}
		})
	})
}

// rustCatPathsModal lists every path inside a category — the "check what
// you are about to delete" step for review items.
func (m *Model) rustCatPathsModal(c *ui.Context, t *ui.Theme) {
	if m.rustPathsCat < 0 || m.rustPathsCat >= disk.RustCatCount || m.rustReport == nil {
		m.rustPathsOpen = false
		return
	}
	cat := &m.rustReport.Cats[m.rustPathsCat]
	ui.Modal(c, &m.rustPathsOpen, func() {
		ui.Column(c).Width(600).MaxHeight(430).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Text(c, cat.Name).Bold().FontSize(16)
			ui.Text(c, cat.Note).FontSize(11).TextColor(t.TextMuted)
			ui.List(c, &m.rustPathsList, len(cat.Dirs), func(i int) {
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

// rustIdleText renders days idle in a compact unit.
func rustIdleText(days int) string {
	switch {
	case days < 0:
		return "—"
	case days == 0:
		return "today"
	case days < 14:
		return fmt.Sprintf("%dd", days)
	case days < 60:
		return fmt.Sprintf("%dw", days/7)
	case days < 365:
		return fmt.Sprintf("%dmo", days/30)
	default:
		return fmt.Sprintf("%dy", days/365)
	}
}

// rustSortItems orders the report rows by the active sort — biggest or
// stalest first.
func (m *Model) rustSortItems() {
	rep := m.rustReport
	if rep == nil {
		return
	}
	if m.rustSortIdle {
		sort.SliceStable(rep.Items, func(i, j int) bool { return rep.Items[i].Idle > rep.Items[j].Idle })
	} else {
		sort.SliceStable(rep.Items, func(i, j int) bool { return rep.Items[i].Total > rep.Items[j].Total })
	}
}

// rustRowButton is the per-row clean control: the first click arms it, the
// second deletes that project's whole target/ dir. Arms cancel when the
// report mutates — the key is the target path, not the row index.
func (m *Model) rustRowButton(c *ui.Context, t *ui.Theme, i int, it disk.RustItem) {
	armed := m.rustRowConfirm == it.Path
	label, tip := "Clean", "Delete this project's target/ dir"
	if armed {
		label, tip = "Sure?", "Click again to delete "+it.Path
	}
	e := ui.Row(c).Height(22).PaddingX(8).Gap(4).AlignItems(ui.Center).Radius(t.Radius).Label("Clean " + it.Path).Tooltip(tip)
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
	if e.Disabled(m.rustBusy).Clicked() {
		if armed {
			m.cleanRustItem(i)
		} else {
			m.rustRowConfirm = it.Path
		}
	}
}
