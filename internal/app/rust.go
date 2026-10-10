package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dumpingground/dumpingground/internal/disk"
	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/egoist/mygo/ui"
)

// rustOrange is the Rust brand color; the toolbar button is icon-only,
// white glyph on orange.
var rustOrange = ui.RGB(0xF7, 0x4C, 0x00)

// rustScan analyzes the current snapshot for Rust build artifacts, or kicks
// off a scan first when there is nothing indexed yet.
func (m *Model) rustScan() {
	if m.diskSnapshot == nil {
		m.rustPending = true
		if m.diskCancel == nil {
			m.startScan()
		}
		return
	}
	m.rustAnalyze()
}

// rustAnalyze runs the artifact detection off the UI goroutine — the walk
// touches every node of a possibly multi-million-node index.
func (m *Model) rustAnalyze() {
	s := m.diskSnapshot
	if s == nil || m.rustBusy {
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
			m.rustConfirm = -1
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
// path still is the expected artifact directory.
func (m *Model) cleanRust(tier int) {
	rep := m.rustReport
	s := m.diskSnapshot
	if rep == nil || s == nil || m.rustBusy || tier < 0 || tier >= len(rep.Tiers) {
		return
	}
	type job struct {
		path  string
		bytes uint64
	}
	jobs := make([]job, 0, len(rep.Tiers[tier].Dirs))
	for _, id := range rep.Tiers[tier].Dirs {
		jobs = append(jobs, job{s.FullPath(id), s.Nodes[id].Allocated})
	}
	m.rustBusy = true
	m.rustMsg = "Deleting…"
	go func() {
		var freed uint64
		var done, failed int
		var firstErr error
		for _, j := range jobs {
			if err := disk.SafeRustDelete(j.path); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := os.RemoveAll(j.path); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			done++
			freed += j.bytes
			m.post(func() { m.rustGone[j.path] = true })
		}
		m.post(func() {
			m.rustBusy = false
			m.rustConfirm = -1
			m.rustMsg = fmt.Sprintf("Deleted %s dirs, freed about %s", format.Int(uint64(done)), format.Bytes(freed))
			if failed > 0 && firstErr != nil {
				m.rustMsg += fmt.Sprintf(" · %s failed: %s", format.Int(uint64(failed)), firstErr)
			}
			m.rustAnalyze()
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

// rustResultsView fills the disk page with the artifact report: one card
// per cleanup tier, then every target/ dir in the shared table.
func (m *Model) rustResultsView(c *ui.Context, t *ui.Theme) {
	rep := m.rustReport
	s := m.diskSnapshot
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
		cols := []ui.TableColumn{
			{ID: "proj", Title: "Project", MinWidth: 220},
			{ID: "incr", Title: "Incremental", Width: 110, Align: ui.End},
			{ID: "debug", Title: "Debug", Width: 110, Align: ui.End},
			{ID: "total", Title: "Total", Width: 110, Align: ui.End},
			{ID: "pct", Title: "%", Width: 70, Align: ui.End},
		}
		total := max(s.Allocated, 1)
		tableChrome(c, func() ui.Element {
			return ui.Table(c, &m.rustList, cols, len(rep.Items), func(i, col int) {
				it := rep.Items[i]
				var str, tip string
				switch col {
				case 0:
					str = s.FullPath(s.Nodes[it.Target].Parent)
					tip = str
				case 1:
					str = format.Bytes(it.Incr)
				case 2:
					str = format.Bytes(it.Debug)
				case 3:
					str = format.Bytes(it.Total)
				case 4:
					str = fmt.Sprintf("%.1f%%", float64(it.Total)/float64(total)*100)
				}
				ui.Text(c, str).SingleLine().Tooltip(tip)
			})
		}).Grow(1).FillWidth()
	})
}

// rustTierCard is one cleanup level: its reclaimable size plus a Clean
// button that asks for a second click before deleting anything.
func (m *Model) rustTierCard(c *ui.Context, t *ui.Theme, i int) {
	tier := &m.rustReport.Tiers[i]
	card := ui.Column(c).Grow(1).Padding(10, 12).Gap(6).Background(t.Surface).Radius(8)
	card.BorderWidth(1).BorderColor(t.Border)
	card.Children(func() {
		ui.Text(c, tier.Name).Bold().FontSize(13)
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
}
