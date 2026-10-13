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
	hwPad     = 16  // page padding
	hwGap     = 12  // gap between panels
	hwRailW   = 300 // default catalogue width
	hwRailMin = 225 // drag limits: ¾ below and ⅓ above the default
	hwRailMax = 400
	hwRowH    = 52 // one catalogue entry
)

// computerView is the Computer page: a catalogue rail on the left, the
// selected card's detail on the right. The first entry is the machine
// itself; m.hw.detail "" selects it, as does "machine".
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
	selID := m.hw.detail
	if selID == "" {
		selID = "machine"
	}
	cd := &cards[0]
	for i := range cards {
		if cards[i].id == selID {
			cd = &cards[i]
			break
		}
	}
	if cd.id != selID {
		m.hw.detail = "" // the hardware went away (a disk was ejected)
		cd = &cards[0]
	}
	railW := m.hw.railW
	if railW <= 0 {
		railW = hwRailW
	}
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		m.hwRail(c, t, cards, cd.id, hist, railW)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			m.hwDetail(c, t, info, hist, cd)
		})
		// The seam is an invisible drag strip: hovering shows a resize
		// cursor and a faint accent hairline; dragging resizes the
		// catalogue within its limits.
		var held bool
		var dx float32
		h := ui.Box(c).Absolute().Top(0).Bottom(0).Left(railW - 3).Width(6).
			Cursor(ui.CursorResizeEW).Label("Resize catalogue")
		h.Draw(func(p *ui.Painter, r ui.Rect) {
			if held || h.Hovered() {
				p.Fill(ui.Rect{X: r.X + r.W/2, Y: r.Y, W: 1, H: r.H}, t.Accent.Alpha(0.55), 0)
			}
		})
		dx, _, held = h.Dragged()
		if held {
			m.hw.railW = min(hwRailMax, max(hwRailMin, railW+dx))
		}
	})
}

// hwRail is the left catalogue: one compact row per card, the machine
// entry first.
func (m *Model) hwRail(c *ui.Context, t *ui.Theme, cards []hwCard, selID string, hist []hwmon.Sample, railW float32) {
	ui.Column(c).Width(railW).Shrink(0).MinHeight(0).Children(func() {
		ui.Scroll(c.Key("hw-rail")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Padding(hwPad, 10).Gap(6).AlignItems(ui.Stretch).Children(func() {
				for i := range cards {
					m.hwRailRow(c, t, &cards[i], hist, cards[i].id == selID, railW)
				}
			})
		})
	})
}

// hwRailRow is one catalogue entry: glyph, name and the live figure on
// the left half, a wide chart thumbnail filling the right half. The
// selected row's edge bar sits clear of the glyph.
func (m *Model) hwRailRow(c *ui.Context, t *ui.Theme, cd *hwCard, hist []hwmon.Sample, selected bool, railW float32) {
	metric := cd.value
	if cd.vsub != "" {
		metric = strings.TrimSpace(metric + "  " + cd.vsub)
	}
	e := ui.Row(c.Key("rail:"+cd.id)).Height(hwRowH).Padding(0, 10, 0, 14).Gap(10).AlignItems(ui.Center).Radius(8).Cursor(ui.CursorPointer).Label(cd.title)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		switch {
		case selected:
			p.Fill(r, cd.col.Alpha(0.10), 8)
			p.Fill(ui.Rect{X: r.X + 4, Y: r.Y + 10, W: 3, H: r.H - 20}, cd.col, 2)
		case e.Hovered():
			p.Fill(r, t.SurfaceHover, 8)
		}
	})
	e.Children(func() {
		icon(c, cd.glyph, 22).TextColor(cd.col)
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cd.title).Bold().FontSize(12).SingleLine().Ellipsis("…")
				if cd.health != "" {
					ui.Box(c).Size(7, 7).Radius(4).Background(hwHealthColor(cd.health, t))
				}
			})
			if metric != "" {
				ui.Text(c, metric).FontSize(10).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
			}
		})
		if len(cd.opts.lines) > 0 {
			ui.Box(c).Width(max(48, (railW-20)/2)).Height(30).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) { drawChart(p, r, t, hist, cd.opts) })
		}
	})
	if e.Clicked() {
		m.hw.detail = cd.id
	}
}

// hwHeader is the machine's identity panel: name, make and model,
// operating system and uptime, then the one-line summary of each major
// part. The machine card's detail shows it.
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

// hwFacts are the machine detail's label/value pairs; parts the machine
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
