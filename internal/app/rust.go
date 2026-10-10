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
			m.rustOpen = true
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

// rustView renders the artifact report and the per-tier clean buttons.
func (m *Model) rustView(c *ui.Context) {
	t := c.Theme()
	ui.Modal(c, &m.rustOpen, func() {
		ui.Column(c).Width(440).Padding(6, 16, 14, 16).Gap(10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				icon(c, "rust", 20)
				ui.Column(c).Grow(1).MinWidth(0).PaddingX(8).Children(func() {
					ui.Text(c, "Rust build artifacts").Bold().FontSize(14)
					ui.Text(c, fmt.Sprintf("%s projects with target/ dirs", format.Int(uint64(m.rustReport.Projects)))).FontSize(11).TextColor(t.TextMuted)
				})
			})
			if m.rustReport.Projects == 0 {
				ui.Text(c, "No Cargo target/ directories in this index.").TextColor(t.TextMuted)
			}
			for i := range m.rustReport.Tiers {
				m.rustTierRow(c, t, i)
			}
			if m.rustMsg != "" {
				ui.Text(c, m.rustMsg).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Deletions apply on disk; the index updates live.").FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0)
				if ui.Button(c, "Rescan").Disabled(m.rustBusy).Clicked() {
					m.rustAnalyze()
				}
			})
		})
	})
}

func (m *Model) rustTierRow(c *ui.Context, t *ui.Theme, i int) {
	tier := &m.rustReport.Tiers[i]
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, tier.Name).Bold()
				ui.Text(c, tier.Note).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
			})
			ui.Text(c, format.Bytes(tier.Bytes)).Width(80).Shrink(0).TextAlign(ui.End)
			if m.dangerButton(c, "trash", "Clean").Disabled(tier.Bytes == 0 || m.rustBusy).Clicked() {
				m.rustConfirm = i
			}
		})
		if m.rustConfirm == i {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, fmt.Sprintf("Delete %s dirs, free %s?", format.Int(uint64(len(tier.Dirs))), format.Bytes(tier.Bytes))).FontSize(11).TextColor(t.Danger).Grow(1).MinWidth(0)
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
