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

// cleanRust removes every directory in the tier after a re-check that each
// path still is the expected artifact directory. The report is
// self-contained, so this works with or without an index.
func (m *Model) cleanRust(tier int) {
	rep := m.rustReport
	if rep == nil || m.rustBusy || tier < 0 || tier >= len(rep.Tiers) {
		return
	}
	// Copy: ApplyDeleted rewrites the tier slices underneath, and the
	// worker goroutine must not share their backing array.
	m.rustDelete(append([]disk.RustDir{}, rep.Tiers[tier].Dirs...))
}

// cleanRustItem removes one project's whole target/ dir — the row-level
// version of tier 3.
func (m *Model) cleanRustItem(i int) {
	rep := m.rustReport
	if rep == nil || m.rustBusy || i < 0 || i >= len(rep.Items) {
		return
	}
	it := rep.Items[i]
	m.rustDelete([]disk.RustDir{{Path: it.Path, Bytes: it.Total}})
}

// rustDelete runs the actual removals off the UI goroutine, then folds the
// outcome back into the report.
func (m *Model) rustDelete(jobs []disk.RustDir) {
	if len(jobs) == 0 {
		return
	}
	m.rustBusy = true
	m.rustMsg = "Deleting…"
	go func() {
		var freed uint64
		var failed int
		var firstErr error
		deleted := map[string]uint64{}
		for _, j := range jobs {
			if err := disk.SafeRustDelete(j.Path); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := os.RemoveAll(j.Path); err != nil {
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
			m.rustMsg = fmt.Sprintf("Deleted %s dirs, freed about %s", format.Int(uint64(len(deleted))), format.Bytes(freed))
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

// rustCovers reports whether cleaning the given tier removes parts of the
// item's target dir — tier 1 incremental caches, tier 2 debug builds,
// tier 3 everything.
func rustCovers(it disk.RustItem, tier int) bool {
	switch tier {
	case 0:
		return it.Incr > 0
	case 1:
		return it.Debug > 0
	}
	return true
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

// rustResultsView fills the disk page with the artifact report: one card
// per cleanup tier, then every target/ dir in the shared table.
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
			for i := range rep.Tiers {
				m.rustTierCard(c, t, i)
			}
		})
		if rep.Projects == 0 {
			ui.Text(c, "No Cargo target/ directories in this index.").TextColor(t.TextMuted)
			return
		}
		// Header matching the row layout below.
		ui.Row(c).FillWidth().Height(24).Shrink(0).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(56).Shrink(0)
			ui.Text(c, "Project").FontSize(11).Bold().TextColor(t.TextMuted).Grow(1).MinWidth(220).SingleLine()
			for _, h := range []string{"Incremental", "Debug", "Total"} {
				ui.Row(c).Width(110).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, h).FontSize(11).Bold().TextColor(t.TextMuted)
				})
			}
			ui.Row(c).Width(70).Shrink(0).Justify(ui.End).Children(func() {
				ui.Text(c, "%").FontSize(11).Bold().TextColor(t.TextMuted)
			})
			ui.Row(c).Width(64).Shrink(0).Justify(ui.End).Children(func() {
				ui.Text(c, "Idle").FontSize(11).Bold().TextColor(t.TextMuted)
			})
			ui.Box(c).Width(76).Shrink(0)
		})
		total := max(rep.Scanned, 1)
		ui.List(c, &m.rustList, len(rep.Items), func(i int) {
			it := rep.Items[i]
			hit := m.rustSel >= 0 && rustCovers(it, m.rustSel)
			row := ui.Row(c).FillWidth().Height(28).Padding(0, 10).Gap(6).AlignItems(ui.Center).Radius(4)
			hovered := row.Hovered()
			switch {
			case hit:
				row.Background(t.Accent.Alpha(0.12))
			case hovered:
				row.Background(t.SurfaceHover)
			}
			row.Children(func() {
				ui.Row(c).Width(56).Shrink(0).Gap(4).Children(func() {
					for tier := range rep.Tiers {
						dim := !rustCovers(it, tier) || (m.rustSel >= 0 && tier != m.rustSel)
						tierBadge(c, t, tier+1, dim)
					}
				})
				path := filepath.Dir(it.Path)
				ptxt := ui.Text(c, path).SingleLine().Tooltip(path).Grow(1).MinWidth(0)
				if it.Idle > 60 {
					ptxt.TextColor(t.TextMuted)
				}
				for _, v := range []uint64{it.Incr, it.Debug, it.Total} {
					ui.Row(c).Width(110).Shrink(0).Justify(ui.End).Children(func() {
						ui.Text(c, format.Bytes(v)).SingleLine()
					})
				}
				ui.Row(c).Width(70).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, fmt.Sprintf("%.1f%%", float64(it.Total)/float64(total)*100)).SingleLine()
				})
				ui.Row(c).Width(64).Shrink(0).Justify(ui.End).Children(func() {
					ui.Text(c, rustIdleText(it.Idle)).SingleLine().TextColor(t.TextMuted)
				})
				ui.Row(c).Width(76).Shrink(0).Justify(ui.End).Children(func() {
					m.rustRowButton(c, t, i, it)
				})
			})
		}).Grow(1).FillWidth()
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

// rustTierCard is one cleanup level: its numbered badge, reclaimable size
// and a Clean button that asks for a second click. Clicking the card
// selects the plan and lights up the files it removes.
func (m *Model) rustTierCard(c *ui.Context, t *ui.Theme, i int) {
	tier := &m.rustReport.Tiers[i]
	on := m.rustSel == i
	card := ui.Column(c).Grow(1).Padding(10, 12).Gap(6).Background(t.Surface).Radius(8)
	card.BorderWidth(1).BorderColor(t.Border).Cursor(ui.CursorPointer)
	if on {
		card.BorderColor(t.Accent)
	}
	card.Tooltip("Click to highlight the files this plan removes")
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			tierBadge(c, t, i+1, false)
			ui.Text(c, tier.Name).Bold().FontSize(13)
		})
		ui.Text(c, tier.Note).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip(tier.Note)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, format.Bytes(tier.Bytes)).FontSize(16).Bold().Grow(1).MinWidth(0)
			if m.dangerButton(c, "trash", "Clean").Disabled(tier.Bytes == 0 || m.rustBusy).Clicked() {
				m.rustConfirm = i
			}
		})
		if m.rustConfirm == i {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, fmt.Sprintf("Delete %s dirs?", format.Int(uint64(len(tier.Dirs))))).FontSize(11).TextColor(t.Danger).Grow(1).MinWidth(0)
				if ui.Button(c, "Delete").Clicked() {
					m.cleanRust(i)
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
			m.rustSel = i
		}
	}
}
