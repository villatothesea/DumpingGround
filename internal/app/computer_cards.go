package app

import (
	"fmt"

	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo/ui"
)

const (
	hwCardMin = 300 // minimum card width before the grid folds a column
	hwCardH   = 188
)

// computerViewCards is the Computer page's original layout: the machine
// header panel on top and a grid of cards underneath, each opening its
// detail when clicked. The Settings page swaps between it and the
// sidebar catalogue.
func (m *Model) computerViewCards(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample, cards []hwCard) {
	if m.hw.detail != "" {
		for i := range cards {
			if cards[i].id == m.hw.detail {
				m.hwDetail(c, t, info, hist, &cards[i])
				return
			}
		}
		m.hw.detail = "" // the hardware went away (a disk was ejected)
	}
	w, _ := c.Size()
	cols := max(1, min(4, int((w-2*hwPad+hwGap)/(hwCardMin+hwGap))))
	ui.Scroll(c.Key("hw-overview")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Padding(hwPad).Gap(hwGap).AlignItems(ui.Stretch).Children(func() {
			m.hwHeader(c, t, info, hist)
			ui.Grid(c).Columns(cols).Gap(hwGap).Children(func() {
				for i := range cards {
					if cards[i].id == "machine" {
						continue // the header panel above plays that role
					}
					m.hwCardView(c, t, &cards[i], hist)
				}
			})
		})
	})
}

// hwCardView is one hardware card: icon and name, the current figure, a
// live chart — or the summary for cards that have no series — and the
// card's subtitle facts. The whole card is the click target.
func (m *Model) hwCardView(c *ui.Context, t *ui.Theme, cd *hwCard, hist []hwmon.Sample) {
	e := ui.Column(c.Key(cd.id)).Height(hwCardH).Padding(12).Gap(8).Radius(10).Background(t.Surface).Cursor(ui.CursorPointer).Label(cd.title)
	e.BorderWidth(1).BorderColor(t.Border)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if e.Hovered() {
			p.Fill(r, t.SurfaceHover, 10)
			p.Stroke(r, cd.col.Alpha(0.7), 10, 1)
		}
	})
	e.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			icon(c, cd.glyph, 18).TextColor(cd.col)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, cd.title).Bold().FontSize(13).SingleLine().Ellipsis("…")
				if cd.health != "" {
					ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(7, 7).Radius(4).Background(hwHealthColor(cd.health, t))
						ui.Text(c, cd.sub).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip(fmt.Sprintf(tr("Health: %s"), tr(cd.health)))
					})
				} else {
					ui.Text(c, cd.sub).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
				}
			})
			ui.Column(c).AlignItems(ui.End).Shrink(0).Children(func() {
				v := ui.Text(c, cd.value).FontSize(18).Bold().SingleLine()
				if cd.vsub != "" {
					v.FontSize(14)
					ui.Text(c, cd.vsub).FontSize(11).TextColor(t.TextMuted).SingleLine()
				}
			})
		})
		if len(cd.opts.lines) > 0 {
			ui.Box(c).Grow(1).MinHeight(0).Draw(func(p *ui.Painter, r ui.Rect) { drawChart(p, r, t, hist, cd.opts) })
		} else {
			ui.Column(c).Grow(1).MinHeight(0).Center().Children(func() {
				ui.Text(c, cd.sub).FontSize(12).TextColor(t.TextMuted).MaxLines(3).Ellipsis("…")
			})
		}
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, tr("Details")).FontSize(11).TextColor(t.TextMuted).SingleLine()
			ui.Spacer(c)
			icon(c, "right", 12).TextColor(t.TextMuted)
		})
	})
	if e.Clicked() {
		m.hw.detail = cd.id
	}
}
