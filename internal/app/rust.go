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
// the user picks a risk level instead of nine separate switches. The
// card shows the bundle as the category badges themselves — numbers on
// this page only ever mean categories. allTargets means "every project
// target dir whole" — the nuke option.
type rustPreset struct {
	name, tag, rebuild string
	cats               []int
	allTargets         bool
}

var rustPresets = []rustPreset{
	{name: "Light", tag: "SAFE", rebuild: "Rebuild: nothing — Cargo re-downloads", cats: []int{disk.RustCatIncr, disk.RustCatDoc, disk.RustCatRegistry, disk.RustCatGit}},
	{name: "Deep", tag: "MODERATE", rebuild: "Rebuild: next cargo build", cats: []int{disk.RustCatIncr, disk.RustCatDoc, disk.RustCatDebug, disk.RustCatRegistry, disk.RustCatGit}},
	{name: "Nuke", tag: "AGGRESSIVE", rebuild: "Rebuild: everything incl. release", allTargets: true, cats: []int{disk.RustCatRegistry, disk.RustCatGit}},
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
	m.rustAnStage, m.rustAnVis, m.rustAnFound = disk.RustStageIndex, 0, 0
	gone := m.rustGoneFn()
	go func() {
		rep := disk.FindRustTargets(s, gone, func(stage, vis, found int) {
			m.post(func() { m.rustAnStage, m.rustAnVis, m.rustAnFound = stage, vis, found })
		})
		m.post(func() {
			m.rustBusy = false
			m.rustReport = rep
			m.rustMode = true
			m.junkMode = false
			m.rustSel = -1
			m.rustConfirm = -1
			m.rustRowConfirm = ""
			m.rustFilterCat = -1
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

// rustRowHit reports whether the selected preset removes this row: every
// project target for the nuke, the preset's own categories otherwise.
func (m *Model) rustRowHit(r rustRow) bool {
	sel := m.rustSel
	if sel < 0 || sel >= len(rustPresets) {
		return false
	}
	pre := rustPresets[sel]
	if r.cat >= 0 {
		for _, cc := range pre.cats {
			if cc == r.cat {
				return true
			}
		}
		return false
	}
	if pre.allTargets {
		return true
	}
	for _, cc := range pre.cats {
		if cc <= disk.RustCatStray && rustCatCovers(*r.item, cc) {
			return true
		}
	}
	return false
}

// rustItemRisk is the worst risk level among the categories a project's
// target/ covers — the row's Clean removes the whole dir, so its tag is
// the most careful level present.
func rustItemRisk(rep *disk.RustReport, it *disk.RustItem) int {
	risk := disk.RustRiskSafe
	for cat := disk.RustCatIncr; cat <= disk.RustCatStray; cat++ {
		if rustCatCovers(*it, cat) && rep.Cats[cat].Risk > risk {
			risk = rep.Cats[cat].Risk
		}
	}
	return risk
}

// rustCatHasRows reports whether any table row covers a category —
// project targets for the in-target categories, shared dirs for the rest.
func (m *Model) rustCatHasRows(cat int) bool {
	rep := m.rustReport
	if cat <= disk.RustCatStray {
		for i := range rep.Items {
			if rustCatCovers(rep.Items[i], cat) {
				return true
			}
		}
		return false
	}
	return len(rep.Cats[cat].Dirs) > 0
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

// rustRow is one row in the cleanup table: a group header (head set), a
// project target (item set), or a shared location (dir+cat set). Rows
// mix projects and shared items — every cleanable path gets a row.
type rustRow struct {
	head  string
	item  *disk.RustItem
	dir   disk.RustDir
	cat   int // shared row's category; -1 for project rows and headers
	risk  int
	bytes uint64
	idle  int
}

// rustRows builds the display list from the report: the category filter
// first, then the active sort, then optional risk-level grouping with
// header rows. Rebuilt per frame — the list is a few dozen entries.
func (m *Model) rustRows() []rustRow {
	rep := m.rustReport
	rows := make([]rustRow, 0, len(rep.Items)+8)
	for i := range rep.Items {
		it := &rep.Items[i]
		if m.rustFilterCat >= 0 && (m.rustFilterCat > disk.RustCatStray || !rustCatCovers(*it, m.rustFilterCat)) {
			continue
		}
		rows = append(rows, rustRow{item: it, cat: -1, risk: rustItemRisk(rep, it), bytes: it.Total, idle: it.Idle})
	}
	for cat := disk.RustCatRegistry; cat <= disk.RustCatOrphans; cat++ {
		if m.rustFilterCat >= 0 && m.rustFilterCat != cat {
			continue
		}
		for _, d := range rep.Cats[cat].Dirs {
			rows = append(rows, rustRow{dir: d, cat: cat, risk: rep.Cats[cat].Risk, bytes: d.Bytes, idle: -1})
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if m.rustSortIdle {
			return rows[a].idle > rows[b].idle
		}
		return rows[a].bytes > rows[b].bytes
	})
	if !m.rustGroupSafe {
		return rows
	}
	grouped := make([]rustRow, 0, len(rows)+4)
	for risk := disk.RustRiskSafe; risk <= disk.RustRiskReview; risk++ {
		var n int
		var bytes uint64
		for _, r := range rows {
			if r.risk == risk {
				n++
				bytes += r.bytes
			}
		}
		if n == 0 {
			continue
		}
		label := "items"
		if n == 1 {
			label = "item"
		}
		grouped = append(grouped, rustRow{head: fmt.Sprintf("%s — %s %s · %s",
			rustRiskLabel(risk), format.Int(uint64(n)), label, format.Bytes(bytes)), cat: -1, risk: risk})
		if m.rustCollapsed[risk] {
			continue
		}
		for _, r := range rows {
			if r.risk == risk {
				grouped = append(grouped, r)
			}
		}
	}
	return grouped
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

// rustAnalyzingView is the placeholder between a finished scan and the
// completed artifact analysis — the index walk reports live node and
// target counts, so this shows real progress instead of a static label.
func (m *Model) rustAnalyzingView(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		card := ui.Column(c).Width(440).Padding(16, 20).Gap(10).Background(t.Surface).Radius(8)
		card.BorderWidth(1).BorderColor(t.Border)
		card.Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				icon(c, "rust", 18)
				ui.Text(c, "Analyzing Rust build artifacts").FontSize(14).Bold()
			})
			frac, detail := -1.0, "Walking the index…"
			switch m.rustAnStage {
			case disk.RustStageIndex:
				detail = fmt.Sprintf("%s nodes · %s targets found",
					format.Int(uint64(m.rustAnVis)), format.Int(uint64(m.rustAnFound)))
				if m.diskSnapshot != nil && len(m.diskSnapshot.Nodes) > 0 {
					frac = float64(m.rustAnVis) / float64(len(m.diskSnapshot.Nodes))
				}
			case disk.RustStageCaches:
				detail = fmt.Sprintf("%s targets · measuring Cargo caches & toolchains…",
					format.Int(uint64(m.rustAnFound)))
			}
			ui.Text(c, detail).FontSize(11).TextColor(t.TextMuted).SingleLine()
			ui.Progress(c, frac).FillWidth().Height(4)
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

// rustRiskNote is the group header's explanation — what membership means
// and what deleting costs.
func rustRiskNote(risk int) string {
	return []string{
		"re-downloaded or regenerated on demand — nothing to rebuild",
		"back after the next cargo build or a rustup reinstall",
		"optimized binaries — the slowest to rebuild; check nothing runs from them",
		"contains files Cargo may not own — check the paths first",
	}[risk]
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

// rustSectionLabel is a muted caption with a hairline — the page's
// visual skeleton: Pick a plan / Or pick exactly / Projects.
func rustSectionLabel(c *ui.Context, t *ui.Theme, s string) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, s).FontSize(11).Bold().TextColor(t.TextMuted).Shrink(0)
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
	})
}

// rustResultsView fills the disk page with the artifact report: three
// preset plans up top, then the unified table — every cleanable path is
// a row, filterable by category badge and groupable by risk level.
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
			}
			if m.iconButton(c, "x", "Back to disk view").Clicked() {
				m.rustMode = false
			}
		})
		rustSectionLabel(c, t, "Pick a plan — or click one to preview what it removes")
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			for p := range rustPresets {
				m.rustPresetCard(c, t, p)
			}
		})
		m.rustFilterStrip(c, t)
		rows := m.rustRows()
		if len(rows) == 0 {
			msg := "No Cargo target/ directories in this index."
			if m.rustFilterCat >= 0 {
				msg = "No rows match this filter."
			}
			ui.Text(c, msg).TextColor(t.TextMuted)
		} else {
			rustSectionLabel(c, t, "Items — every cleanable path, one row each")
			// Header matching the row layout below.
			ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(176).Shrink(0)
				ui.Row(c).Width(78).Shrink(0).Children(func() {
					ui.Text(c, "Risk").FontSize(11).Bold().TextColor(t.TextMuted)
				})
				ui.Text(c, "Path").FontSize(11).Bold().TextColor(t.TextMuted).Grow(1).MinWidth(120).SingleLine()
				for _, h := range []string{"Incr", "Debug", "Release", "Total"} {
					ui.Row(c).Width(92).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, h).FontSize(11).Bold().TextColor(t.TextMuted)
					})
				}
				ui.Row(c).Width(56).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, "%").FontSize(11).Bold().TextColor(t.TextMuted)
				})
				ui.Row(c).Width(52).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, "Idle").FontSize(11).Bold().TextColor(t.TextMuted)
				})
				ui.Box(c).Width(76).Shrink(0)
			})
			total := max(rep.Scanned, 1)
			ui.List(c, &m.rustList, len(rows), func(i int) {
				r := rows[i]
				if r.head != "" {
					collapsed := m.rustCollapsed[r.risk]
					g := "caret-d"
					if collapsed {
						g = "caret-r"
					}
					he := ui.Row(c).FillWidth().Height(26).Padding(0, 10).Gap(6).AlignItems(ui.Center).Radius(4).
						Cursor(ui.CursorPointer).Label("Toggle group " + r.head)
					if he.Hovered() {
						he.Background(t.SurfaceHover)
					}
					he.Children(func() {
						icon(c, g, 10).TextColor(t.TextMuted)
						ui.Text(c, r.head).FontSize(11).Bold().TextColor(rustRiskColor(t, r.risk)).Shrink(0)
						ui.Text(c, "· "+rustRiskNote(r.risk)).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
					})
					if he.Clicked() {
						m.rustCollapsed[r.risk] = !collapsed
					}
					return
				}
				hit := m.rustRowHit(r)
				row := ui.Row(c).FillWidth().Height(28).Padding(0, 10).Gap(6).AlignItems(ui.Center).Radius(4)
				hovered := row.Hovered()
				switch {
				case hit:
					row.Background(t.Accent.Alpha(0.12))
				case hovered:
					row.Background(t.SurfaceHover)
				}
				row.Children(func() {
					// Nine badge slots — a badge lights where the row
					// covers that category. The filter dims all but its own.
					ui.Row(c).Width(176).Shrink(0).Gap(4).Children(func() {
						for cat := 0; cat < disk.RustCatCount; cat++ {
							lit := cat == r.cat
							if r.cat < 0 {
								lit = rustCatCovers(*r.item, cat)
							}
							dim := !lit
							if m.rustFilterCat >= 0 {
								dim = cat != m.rustFilterCat
							}
							tierBadge(c, t, cat+1, dim).Tooltip(rep.Cats[cat].Name)
						}
					})
					ui.Row(c).Width(78).Shrink(0).Children(func() {
						ui.Text(c, rustRiskLabel(r.risk)).FontSize(10).Bold().TextColor(rustRiskColor(t, r.risk))
					})
					if r.cat >= 0 {
						ui.Text(c, rep.Cats[r.cat].Name).FontSize(11).TextColor(t.Accent).Shrink(0)
						ui.Text(c, r.dir.Path).SingleLine().Tooltip(r.dir.Path).Grow(1).MinWidth(0).TextColor(t.TextMuted)
					} else {
						path := filepath.Dir(r.item.Path)
						ptxt := ui.Text(c, path).SingleLine().Tooltip(path).Grow(1).MinWidth(0)
						if r.item.Idle > 60 {
							ptxt.TextColor(t.TextMuted)
						}
					}
					vals := []string{"—", "—", "—"}
					if r.cat < 0 {
						vals = []string{format.Bytes(r.item.Incr), format.Bytes(r.item.Debug), format.Bytes(r.item.Rel)}
					}
					for _, v := range vals {
						ui.Row(c).Width(92).Shrink(0).Justify(ui.End).Children(func() {
							ui.Text(c, v).SingleLine()
						})
					}
					ui.Row(c).Width(92).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, format.Bytes(r.bytes)).SingleLine()
					})
					ui.Row(c).Width(56).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, fmt.Sprintf("%.1f%%", float64(r.bytes)/float64(total)*100)).SingleLine()
					})
					ui.Row(c).Width(52).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, rustIdleText(r.idle)).SingleLine().TextColor(t.TextMuted)
					})
					ui.Row(c).Width(76).Shrink(0).Justify(ui.End).Children(func() {
						m.rustRowButton(c, t, r)
					})
				})
			}).Grow(1).FillWidth()
		}
	})
	m.rustCatPathsModal(c, t)
}

// rustFilterStrip is the category filter row: one clickable badge per
// category — click to show only the rows that category touches, click
// again to clear. The right side holds the group-by-safety toggle.
func (m *Model) rustFilterStrip(c *ui.Context, t *ui.Theme) {
	rep := m.rustReport
	ui.Row(c).FillWidth().Height(26).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Filter:").FontSize(11).TextColor(t.TextMuted).Shrink(0)
		for cat := 0; cat < disk.RustCatCount; cat++ {
			cat := cat
			empty := !m.rustCatHasRows(cat)
			dim := empty || (m.rustFilterCat >= 0 && m.rustFilterCat != cat)
			b := tierBadge(c, t, cat+1, dim).Tooltip(rep.Cats[cat].Name + " — filter rows").Cursor(ui.CursorPointer)
			if b.Clicked() && !empty {
				if m.rustFilterCat == cat {
					m.rustFilterCat = -1
				} else {
					m.rustFilterCat = cat
				}
			}
		}
		ui.Checkbox(c, &m.rustGroupSafe, "Group by safety")
		if m.rustFilterCat >= 0 {
			cat := m.rustFilterCat
			ui.Text(c, "· "+rep.Cats[cat].Name).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			if len(rep.Cats[cat].Dirs) > 0 {
				m.rustPathsLink(c, t, cat)
			}
			if m.iconButton(c, "x", "Clear filter").Clicked() {
				m.rustFilterCat = -1
			}
		}
		ui.Spacer(c)
	})
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
			ui.Text(c, pre.name).Bold().FontSize(13)
			if p == 0 {
				ui.Text(c, "recommended").FontSize(10).TextColor(t.Accent)
			}
			ui.Spacer(c)
			ui.Text(c, pre.tag).FontSize(10).Bold().TextColor(tagColor)
		})
		ui.Text(c, format.Bytes(bytes)).FontSize(18).Bold()
		// The plan as the category badges it bundles — numbers are only
		// ever categories, nowhere else.
		ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
			if pre.allTargets {
				ui.Text(c, "every target").FontSize(10).Bold().TextColor(t.Text)
			}
			for _, cat := range pre.cats {
				tierBadge(c, t, cat+1, false).Tooltip(m.rustReport.Cats[cat].Name)
			}
			ui.Text(c, "· "+pre.rebuild).FontSize(10).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip(pre.rebuild)
		})
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
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

// rustPathsLink is the quiet "N paths" affordance opening the category's
// path-list modal — a muted text link, not a button.
func (m *Model) rustPathsLink(c *ui.Context, t *ui.Theme, i int) {
	cat := &m.rustReport.Cats[i]
	e := ui.Row(c).Shrink(0).Gap(3).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label("Show paths in " + cat.Name)
	e.Children(func() {
		label := "path"
		if len(cat.Dirs) != 1 {
			label = "paths"
		}
		ui.Text(c, fmt.Sprintf("%s %s", format.Int(uint64(len(cat.Dirs))), label)).FontSize(11).TextColor(t.Accent)
	})
	if e.Hovered() {
		e.Background(t.SurfaceHover)
	}
	if e.Clicked() {
		m.rustPathsCat = i
		m.rustPathsOpen = true
	}
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

// rustRowButton is the per-row clean control: the first click arms it,
// the second deletes that row's path — a project's whole target/ dir or
// one shared location. Arms cancel when the report mutates — the key is
// the path, not the row index.
func (m *Model) rustRowButton(c *ui.Context, t *ui.Theme, r rustRow) {
	var path string
	var job []disk.RustDir
	if r.cat >= 0 {
		path = r.dir.Path
		job = []disk.RustDir{r.dir}
	} else {
		path = r.item.Path
		job = []disk.RustDir{{Path: r.item.Path, Bytes: r.item.Total, Base: "target"}}
	}
	armed := m.rustRowConfirm == path
	label, tip := "Clean", "Delete "+path
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
	if e.Disabled(m.rustBusy).Clicked() {
		if armed {
			m.rustDelete(job)
		} else {
			m.rustRowConfirm = path
		}
	}
}
