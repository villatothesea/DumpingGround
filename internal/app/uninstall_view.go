package app

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

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
		ui.Box(c).Width(150).Shrink(0)
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
	ui.Row(c).Width(150).Shrink(0).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
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
			} else {
				m.uninstMsg = "Uninstaller launched for " + e.Name + " — refresh when it finishes"
			}
		}
	})
}
