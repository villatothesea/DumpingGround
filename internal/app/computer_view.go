package app

import (
	"fmt"
	"math"
	"strings"

	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo/ui"
)

const (
	hwPad     = 16 // page padding
	hwGap     = 12 // gap between cards and panels
	hwCardMin = 300
	hwCardH   = 188
)

// computerView is the Computer page: the overview of cards, or the detail
// of the one that was clicked.
func (m *Model) computerView(c *ui.Context, t *ui.Theme) {
	info := m.hw.info.Load()
	if info == nil {
		ui.Column(c).Grow(1).MinHeight(0).Center().Children(func() {
			ui.Text(c, "Reading hardware…").TextColor(t.TextMuted)
		})
		return
	}
	hist := m.hwSnapshot()
	cards := hwCards(info, hist)
	if m.hw.detail != "" {
		for i := range cards {
			if cards[i].id == m.hw.detail {
				m.hwDetail(c, t, info, hist, &cards[i])
				return
			}
		}
		m.hw.detail = "" // the hardware went away (a disk was ejected)
	}
	m.hwOverview(c, t, info, hist, cards)
}

func (m *Model) hwOverview(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample, cards []hwCard) {
	w, _ := c.Size()
	cols := max(1, min(4, int((w-2*hwPad+hwGap)/(hwCardMin+hwGap))))
	ui.Scroll(c.Key("hw-overview")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Padding(hwPad).Gap(hwGap).AlignItems(ui.Stretch).Children(func() {
			m.hwHeader(c, t, info, hist)
			ui.Grid(c).Columns(cols).Gap(hwGap).Children(func() {
				for i := range cards {
					m.hwCardView(c, t, &cards[i], hist)
				}
			})
		})
	})
}

// hwHeader is the machine's identity: name, make and model, operating
// system and uptime, then the one-line summary of each major part.
func (m *Model) hwHeader(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample) {
	w, _ := c.Size()
	facts := hwFacts(info)
	cols := 2
	switch {
	case w >= 1100:
		cols = 4
	case w >= 800:
		cols = 3
	}
	panel := ui.Column(c).Padding(16).Gap(14).Radius(10).Background(t.Surface)
	panel.BorderWidth(1).BorderColor(t.Border)
	panel.Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			glyphName := "monitor"
			if info.Battery {
				glyphName = "laptop"
			}
			ui.Box(c).Size(44, 44).Radius(12).Center().Background(hwCPUCol.Alpha(0.14)).Children(func() {
				icon(c, glyphName, 24).TextColor(hwCPUCol)
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, info.Host).FontSize(20).Bold().SingleLine().Ellipsis("…")
				if dev := strings.TrimSpace(info.Manufacturer + " " + info.Model); dev != "" {
					ui.Text(c, dev).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
				}
			})
			ui.Column(c).AlignItems(ui.End).Shrink(0).Children(func() {
				if len(hist) > 0 {
					ui.Text(c, "Up "+durHM(hist[len(hist)-1].Uptime)).SingleLine()
				}
				if info.User != "" {
					ui.Text(c, "Signed in as "+info.User).TextColor(t.TextMuted).SingleLine()
				}
			})
		})
		ui.Divider(c)
		ui.Grid(c).Columns(cols).GapX(24).GapY(12).Children(func() {
			for _, f := range facts {
				m.hwFact(c, t, f[0], f[1])
			}
		})
	})
}

func (m *Model) hwFact(c *ui.Context, t *ui.Theme, label, value string) {
	ui.Column(c).Gap(2).MinWidth(0).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, value).MaxLines(2).Ellipsis("…")
	})
}

// hwFacts are the overview header's label/value pairs; parts the machine
// does not have, or the firmware did not describe, are left out.
func hwFacts(info *hwmon.Info) [][2]string {
	var f [][2]string
	add := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			f = append(f, [2]string{k, v})
		}
	}
	os := strings.TrimSpace(info.OS + " " + info.OSVersion)
	if info.OSBuild != "" {
		os += " (build " + info.OSBuild + ")"
	}
	add("Operating system", os)
	add("Processor", fmt.Sprintf("%s · %s, %d threads", shortCPU(info.CPU.Name), cpuCores(info.CPU), info.CPU.Threads))
	add("Installed memory", memSummary(info))
	var gpus []string
	for _, g := range info.GPUs {
		gpus = append(gpus, deTM(g.Name))
	}
	add("Graphics", strings.Join(gpus, "\n"))
	var size uint64
	for _, d := range info.Disks {
		size += d.Size
	}
	if len(info.Disks) > 0 {
		add("Storage", fmt.Sprintf("%d disk%s · %s", len(info.Disks), plural(len(info.Disks)), format.Bytes(size)))
	}
	if len(info.NICs) > 0 {
		n := info.NICs[0]
		ip := ""
		if len(n.IPs) > 0 {
			ip = " · " + n.IPs[0]
		}
		add("Network", n.Name+ip)
	}
	add("Motherboard", strings.TrimSpace(info.BoardVendor+" "+info.BoardProduct))
	bios := strings.TrimSpace(info.BIOSVendor + " " + info.BIOSVersion)
	if info.BIOSDate != "" && bios != "" {
		bios += " · " + info.BIOSDate
	}
	add("BIOS", bios)
	return f
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// cpuCores names the core count, spelling out a hybrid P/E split.
func cpuCores(cpu hwmon.CPUInfo) string {
	if cpu.PerfCores > 0 || cpu.EffCores > 0 {
		return fmt.Sprintf("%d cores (%d P + %d E)", cpu.Cores, cpu.PerfCores, cpu.EffCores)
	}
	return fmt.Sprintf("%d cores", cpu.Cores)
}

// capWh renders a battery capacity that the driver reports in mWh.
func capWh(mwh uint64) string {
	if mwh == 0 {
		return ""
	}
	return fmt.Sprintf("%.1f Wh", float64(mwh)/1000)
}

// sizeInches is a panel's physical diagonal from its width and height in
// centimetres, as EDID reports them.
func sizeInches(wcm, hcm int) string {
	if wcm <= 0 || hcm <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f in", math.Hypot(float64(wcm), float64(hcm))/2.54)
}

func cmPair(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	return fmt.Sprintf("%d × %d cm", w, h)
}

// hwCardView is one hardware card: icon and name, the current figure, a
// live chart and a few facts. The whole card is the click target.
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
						ui.Text(c, cd.sub).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Tooltip("Health: " + cd.health)
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
			ui.Column(c).Grow(1).MinHeight(0).Gap(4).Children(func() {
				for _, row := range cd.rows {
					if row[0] == "" {
						ui.Text(c, row[1]).FontSize(11).SingleLine().Ellipsis("…")
						continue
					}
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, row[0]).FontSize(11).TextColor(t.TextMuted).Shrink(0)
						ui.Spacer(c)
						ui.Text(c, row[1]).FontSize(11).SingleLine().Ellipsis("…")
					})
				}
			})
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			for _, s := range cd.stats {
				ui.Text(c, s).FontSize(11).TextColor(t.TextMuted).SingleLine()
			}
			ui.Spacer(c)
			icon(c, "right", 12).TextColor(t.TextMuted)
		})
	})
	if e.Clicked() {
		m.hw.detail = cd.id
	}
}
