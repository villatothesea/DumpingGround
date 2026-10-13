package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo/ui"
)

// hwDetail is the page behind a card: a bar with the Back button, then
// big charts, live figures and the static specification.
func (m *Model) hwDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample, cd *hwCard) {
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		// The machine page's own header panel carries its identity; the
		// back bar belongs to the hardware pages only.
		if cd.id != "machine" {
			ui.Row(c).Padding(12, hwPad).Gap(12).AlignItems(ui.Center).Children(func() {
				if m.iconTextButton(c, "left", "Back").Clicked() {
					m.hw.detail = ""
				}
				icon(c, cd.glyph, 20).TextColor(cd.col)
				ui.Text(c, cd.title).FontSize(18).Bold().SingleLine()
				ui.Text(c, cd.sub).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				// The live figure sits in a fixed-width box between the
				// title and the actions: its digits can change width
				// without pushing the buttons around.
				ui.Column(c).Width(200).AlignItems(ui.Center).Shrink(0).Children(func() {
					ui.Text(c, cd.value).FontSize(22).Bold().SingleLine()
					if cd.vsub != "" {
						ui.Text(c, cd.vsub).FontSize(12).TextColor(t.TextMuted)
					}
				})
				// Disk pages carry their page-jump actions in the bar.
				var didx int
				if sscan(cd.id, "disk:%d", &didx) && didx < len(info.Disks) {
					for _, l := range info.Disks[didx].Letters {
						l := l
						if m.iconTextButton(c, "hdd", "Scan "+l+`\`+" in Disk Usage").Clicked() {
							m.diskDrive, m.diskPath = l+`\`, l+`\`
							m.setPage(diskPage)
							m.startScan()
						}
					}
					if m.iconTextButton(c, "pkg", "Open Uninstall").Clicked() {
						m.setPage(uninstallPage)
					}
				}
			})
		}
		topPad := float32(0)
		if cd.id == "machine" {
			topPad = hwPad
		}
		ui.Scroll(c.Key("hw-detail:" + cd.id)).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Padding(topPad, hwPad, hwPad, hwPad).Gap(hwGap).AlignItems(ui.Stretch).Children(func() {
				var idx int
				switch {
				case cd.id == "machine":
					m.hwMachineDetail(c, t, info, hist)
				case cd.id == "cpu":
					m.hwCPUDetail(c, t, info, hist, cd)
				case cd.id == "mem":
					m.hwMemDetail(c, t, info, hist)
				case sscan(cd.id, "gpu:%d", &idx):
					m.hwGPUDetail(c, t, info, hist, cd, idx)
				case sscan(cd.id, "disk:%d", &idx):
					m.hwDiskDetail(c, t, info, hist, idx)
				case sscan(cd.id, "net:%d", &idx):
					m.hwNetDetail(c, t, info, hist, cd, uint32(idx))
				case cd.id == "battery":
					m.hwBatteryDetail(c, t, info, hist, cd)
				case sscan(cd.id, "display:%d", &idx):
					m.hwDisplayDetail(c, t, info, idx)
				case cd.id == "audio":
					m.hwAudioDetail(c, t, info)
				case cd.id == "devices":
					m.hwDevicesDetail(c, t, info)
				}
			})
		})
	})
}

func sscan(id, pattern string, idx *int) bool {
	n, err := fmt.Sscanf(id, pattern, idx)
	return err == nil && n == 1
}

func lastSample(h []hwmon.Sample) *hwmon.Sample {
	if len(h) == 0 {
		return nil
	}
	return &h[len(h)-1]
}

// ---- building blocks ----

func (m *Model) hwPanel(c *ui.Context, t *ui.Theme, build func()) {
	p := ui.Column(c).Padding(14).Gap(10).Radius(10).Background(t.Surface)
	p.BorderWidth(1).BorderColor(t.Border)
	p.Children(build)
}

type hwLegend struct {
	label string
	col   ui.Color
}

// hwChartPanel is a titled live chart with its y-axis maximum and the
// 60-second span labelled.
func (m *Model) hwChartPanel(c *ui.Context, t *ui.Theme, hist []hwmon.Sample, title, top string, o chartOpts, h float32, legend ...hwLegend) {
	o.grid = true
	m.hwPanel(c, t, func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).Bold().FontSize(13)
			ui.Spacer(c)
			for _, l := range legend {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(8, 8).Radius(4).Background(l.col)
					ui.Text(c, l.label).FontSize(11).TextColor(t.TextMuted)
				})
			}
			ui.Text(c, "60 seconds").FontSize(11).TextColor(t.TextMuted)
		})
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			ui.Text(c, top).FontSize(11).TextColor(t.TextMuted)
		})
		ui.Box(c).FillWidth().Height(h).Draw(func(p *ui.Painter, r ui.Rect) { drawChart(p, r, t, hist, o) })
		ui.Row(c).Children(func() {
			ui.Text(c, "60 s").FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.Text(c, "0").FontSize(11).TextColor(t.TextMuted)
		})
	})
}

// hwStatGrid lays out live figures: a small label over a bold value.
func (m *Model) hwStatGrid(c *ui.Context, t *ui.Theme, items [][2]string) {
	w, _ := c.Size()
	cols := max(2, min(6, int((w-2*hwPad-28)/150)))
	m.hwPanel(c, t, func() {
		ui.Grid(c).Columns(cols).GapX(16).GapY(14).Children(func() {
			for _, it := range items {
				m.stat(c, t, it[0], it[1])
			}
		})
	})
}

// hwSpecs is a key/value table; rows without a value are skipped.
func (m *Model) hwSpecs(c *ui.Context, t *ui.Theme, title string, rows [][2]string) {
	m.hwPanel(c, t, func() {
		ui.Text(c, title).Bold().FontSize(13)
		ui.Grid(c).ColumnTracks(ui.Fixed(170), ui.Fr(1)).GapX(16).GapY(8).Children(func() {
			for _, r := range rows {
				if strings.TrimSpace(r[1]) == "" {
					continue
				}
				ui.Text(c, r[0]).TextColor(t.TextMuted)
				ui.Text(c, r[1]).Selectable()
			}
		})
	})
}

// hwMiniCharts is a grid of small labelled charts — logical processors,
// GPU engines.
func (m *Model) hwMiniCharts(c *ui.Context, t *ui.Theme, hist []hwmon.Sample, title string, names []string, line func(i int) hwLine, now func(i int) string, col ui.Color) {
	w, _ := c.Size()
	cols := max(1, min(8, int((w-2*hwPad-28+8)/168)))
	m.hwPanel(c, t, func() {
		ui.Text(c, title).Bold().FontSize(13)
		ui.Grid(c).Columns(cols).Gap(8).Children(func() {
			for i := range names {
				i := i
				ui.Column(c).Gap(3).Children(func() {
					ui.Row(c).Children(func() {
						ui.Text(c, names[i]).FontSize(11).TextColor(t.TextMuted)
						ui.Spacer(c)
						ui.Text(c, now(i)).FontSize(11)
					})
					ui.Box(c).FillWidth().Height(42).Draw(func(p *ui.Painter, r ui.Rect) {
						drawChart(p, r, t, hist, chartOpts{max: 100, col: col, lines: []hwLine{line(i)}})
					})
				})
			}
		})
	})
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v) }

func ghz(mhz int) string {
	if mhz <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2f GHz", float64(mhz)/1000)
}

func sizeOrBlank(v uint64) string {
	if v == 0 {
		return ""
	}
	return format.Bytes(v)
}

// ---- per-hardware pages ----

// hwMachineDetail is the first catalogue entry's page: the machine's
// identity and the whole system's vitals at a glance.
func (m *Model) hwMachineDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample) {
	m.hwHeader(c, t, info, hist)
	stats := [][2]string{{"Up time", "—"}, {"CPU busy", "—"}, {"Memory in use", "—"}}
	if last := lastSample(hist); last != nil {
		stats = [][2]string{
			{"Up time", durHM(last.Uptime)},
			{"CPU busy", pct(last.CPU)},
			{"Memory in use", fmt.Sprintf("%.0f / %.0f GB", float64(last.MemUsed)/(1<<30), float64(last.MemTotal)/(1<<30))},
		}
		var busiest float64
		for _, g := range last.GPUs {
			if g.HasUtilization && g.Util > busiest {
				busiest = g.Util
			}
		}
		if busiest > 0 {
			stats = append(stats, [2]string{"Busiest GPU", pct(busiest)})
		}
	}
	m.hwStatGrid(c, t, stats)
}

func (m *Model) hwCPUDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample, cd *hwCard) {
	last := lastSample(hist)
	m.hwChartPanel(c, t, hist, "Utilization", "100%", cd.opts, 200, hwLegend{"Total", hwCPUCol})
	stats := [][2]string{{"Utilization", "—"}, {"Speed", "—"}, {"Processes", "—"}, {"Threads", "—"}, {"Handles", "—"}, {"Up time", "—"}}
	cores := info.CPU.Threads
	if last != nil {
		stats = [][2]string{{"Utilization", pct(last.CPU)}, {"Speed", ghz(last.MHz)}, {"Processes", format.Int(uint64(last.Procs))},
			{"Threads", format.Int(uint64(last.Threads))}, {"Handles", format.Int(uint64(last.Handles))}, {"Up time", durHM(last.Uptime)}}
		cores = len(last.Cores)
	}
	m.hwStatGrid(c, t, stats)
	names := make([]string, cores)
	for i := range names {
		names[i] = fmt.Sprintf("CPU %d", i)
	}
	m.hwMiniCharts(c, t, hist, "Logical processors", names,
		func(i int) hwLine {
			return hwLine{col: hwCPUCol, fill: true, val: func(s *hwmon.Sample) (float64, bool) {
				if i < len(s.Cores) {
					return s.Cores[i], true
				}
				return 0, false
			}}
		},
		func(i int) string {
			if last != nil && i < len(last.Cores) {
				return pct(last.Cores[i])
			}
			return ""
		}, hwCPUCol)
	cpu := info.CPU
	m.hwSpecs(c, t, "Specifications", [][2]string{
		{"Processor", strings.Join(strings.Fields(cpu.Name), " ")},
		{"Vendor", cpu.Vendor},
		{"Sockets", fmt.Sprint(cpu.Sockets)},
		{"Cores", cpuCores(cpu)},
		{"Logical processors", fmt.Sprint(cpu.Threads)},
		{"Base speed", ghz(cpu.BaseMHz)},
		{"L1 cache", sizeOrBlank(cpu.L1)},
		{"L2 cache", sizeOrBlank(cpu.L2)},
		{"L3 cache", sizeOrBlank(cpu.L3)},
	})
}

func (m *Model) hwMemDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample) {
	last := lastSample(hist)
	total := float64(info.Mem.Total)
	if last != nil && last.MemTotal > 0 {
		total = float64(last.MemTotal)
	}
	used := hwLine{col: hwMemCol, fill: true, val: func(s *hwmon.Sample) (float64, bool) { return float64(s.MemUsed), s.MemTotal > 0 }}
	m.hwChartPanel(c, t, hist, "Memory usage", format.Bytes(uint64(total)), chartOpts{max: total, col: hwMemCol, lines: []hwLine{used}}, 200, hwLegend{"In use", hwMemCol})
	stats := [][2]string{{"In use", "—"}, {"Available", "—"}, {"Committed", "—"}, {"Cached", "—"}, {"Paged pool", "—"}, {"Non-paged pool", "—"}}
	if last != nil {
		stats = [][2]string{{"In use", format.Bytes(last.MemUsed)}, {"Available", format.Bytes(last.MemAvail)},
			{"Committed", format.Bytes(last.Commit) + " / " + format.Bytes(last.CommitLimit)}, {"Cached", format.Bytes(last.MemCached)},
			{"Paged pool", format.Bytes(last.PagedPool)}, {"Non-paged pool", format.Bytes(last.NonPagedPool)}}
		m.hwPanel(c, t, func() {
			ui.Row(c).Children(func() {
				ui.Text(c, "Composition").Bold().FontSize(13)
				ui.Spacer(c)
				ui.Text(c, fmt.Sprintf("%s in use · %s available", format.Bytes(last.MemUsed), format.Bytes(last.MemAvail))).FontSize(11).TextColor(t.TextMuted)
			})
			frac := float64(last.MemUsed) / max(1, float64(last.MemTotal))
			ui.Box(c).FillWidth().Height(16).Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, hwMemCol.Alpha(0.16), 5)
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(frac), H: r.H}, hwMemCol, 5)
			})
		})
	}
	m.hwStatGrid(c, t, stats)
	mem := info.Mem
	slots := ""
	if mem.Slots > 0 {
		slots = fmt.Sprintf("%d of %d used", len(mem.Modules), mem.Slots)
	}
	maxCap := ""
	if mem.MaxCapacity > 0 {
		maxCap = format.Bytes(mem.MaxCapacity)
	}
	m.hwSpecs(c, t, "Specifications", [][2]string{
		{"Installed", memSummary(info)},
		{"Usable by Windows", format.Bytes(mem.Total)},
		{"Modules", memConfig(mem.Modules)},
		{"Slots", slots},
		{"Maximum supported", maxCap},
	})
	if len(mem.Modules) > 0 {
		m.hwPanel(c, t, func() {
			ui.Text(c, "Memory modules").Bold().FontSize(13)
			ui.Grid(c).ColumnTracks(ui.Fr(2.2), ui.Fr(0.8), ui.Fr(0.9), ui.Fr(1.2), ui.Fr(1.3), ui.Fr(2)).GapX(12).GapY(6).Children(func() {
				for _, h := range []string{"Slot", "Size", "Type", "Speed", "Manufacturer", "Part number"} {
					ui.Text(c, h).FontSize(11).TextColor(t.TextMuted)
				}
				for _, d := range mem.Modules {
					speed := d.ConfigMTs
					if speed == 0 {
						speed = d.SpeedMTs
					}
					sp := ""
					if speed > 0 {
						sp = fmt.Sprintf("%d MT/s", speed)
					}
					for _, cell := range []string{d.Slot, format.Bytes(d.Size), cmp.Or(d.Type, "—"), cmp.Or(sp, "—"), cmp.Or(d.Manufacturer, "—"), cmp.Or(d.PartNo, "—")} {
						ui.Text(c, cell).SingleLine().Ellipsis("…")
					}
				}
			})
		})
	}
}

// engineOrder is how GPU engine types are listed; the rest follow
// alphabetically.
var engineOrder = []string{"3D", "Compute", "Copy", "VideoDecode", "VideoEncode", "VideoProcessing"}

func prettyEngine(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' && s[i-1] >= 'a' && s[i-1] <= 'z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ReplaceAll(b.String(), "_", " ")
}

func (m *Model) hwGPUDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample, cd *hwCard, i int) {
	if i >= len(info.GPUs) {
		return
	}
	g := info.GPUs[i]
	last := lastSample(hist)
	m.hwChartPanel(c, t, hist, "Utilization", "100%", cd.opts, 200, hwLegend{"Busiest engine", hwGPUCol})

	// Engine types that did any work in the window, in a stable order.
	seen := map[string]bool{"3D": true}
	for k := range hist {
		if i < len(hist[k].GPUs) {
			for typ, v := range hist[k].GPUs[i].Engines {
				if v > 0 {
					seen[typ] = true
				}
			}
		}
	}
	var engines []string
	for typ := range seen {
		engines = append(engines, typ)
	}
	slices.SortFunc(engines, func(a, b string) int {
		ia, ib := slices.Index(engineOrder, a), slices.Index(engineOrder, b)
		if ia < 0 {
			ia = len(engineOrder)
		}
		if ib < 0 {
			ib = len(engineOrder)
		}
		return cmp.Or(cmp.Compare(ia, ib), cmp.Compare(a, b))
	})
	// gpuLine reads one field of this GPU's sample, tolerating ticks that
	// skipped the GPU query.
	gpuLine := func(col ui.Color, fill bool, f func(gs *hwmon.GPUSample) (float64, bool)) hwLine {
		return hwLine{col: col, fill: fill, val: func(s *hwmon.Sample) (float64, bool) {
			if i >= len(s.GPUs) {
				return 0, false
			}
			return f(&s.GPUs[i])
		}}
	}
	names := make([]string, len(engines))
	for k, e := range engines {
		names[k] = prettyEngine(e)
	}
	m.hwMiniCharts(c, t, hist, "Engines", names,
		func(k int) hwLine {
			return gpuLine(hwGPUCol, true, func(gs *hwmon.GPUSample) (float64, bool) { return gs.Engines[engines[k]], gs.HasUtilization })
		},
		func(k int) string {
			if last != nil && i < len(last.GPUs) {
				return pct(last.GPUs[i].Engines[engines[k]])
			}
			return ""
		}, hwGPUCol)

	sharedCol := hwGPUCol.Mix(ui.Hex("#3b82f6"), 0.6)
	o := chartOpts{floor: 256 << 20, col: hwGPUCol, lines: []hwLine{
		gpuLine(hwGPUCol, true, func(gs *hwmon.GPUSample) (float64, bool) { return float64(gs.DedicatedUsed), gs.HasMemory }),
		gpuLine(sharedCol, false, func(gs *hwmon.GPUSample) (float64, bool) { return float64(gs.SharedUsed), gs.HasMemory }),
	}}
	if g.DedicatedBytes >= 1<<30 {
		o.max = float64(g.DedicatedBytes)
	}
	m.hwChartPanel(c, t, hist, "GPU memory", format.Bytes(uint64(chartScale(hist, o))), o, 140, hwLegend{"Dedicated", hwGPUCol}, hwLegend{"Shared", sharedCol})

	stats := [][2]string{{"Utilization", "—"}, {"Dedicated memory", "—"}, {"Shared memory", "—"}}
	if last != nil && i < len(last.GPUs) {
		gs := last.GPUs[i]
		util := "—"
		if gs.HasUtilization {
			util = pct(gs.Util)
		}
		stats = [][2]string{{"Utilization", util},
			{"Dedicated memory", format.Bytes(gs.DedicatedUsed) + " / " + format.Bytes(g.DedicatedBytes)},
			{"Shared memory", format.Bytes(gs.SharedUsed) + " / " + format.Bytes(g.SharedBytes)}}
	}
	m.hwStatGrid(c, t, stats)
	m.hwSpecs(c, t, "Specifications", [][2]string{
		{"Adapter", deTM(g.Name)},
		{"Vendor", g.Vendor},
		{"Driver version", g.Driver},
		{"Driver date", g.DriverDate},
		{"Dedicated memory", sizeOrBlank(g.DedicatedBytes)},
		{"Shared memory", sizeOrBlank(g.SharedBytes)},
	})
}

func (m *Model) hwDiskDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample, i int) {
	if i >= len(info.Disks) {
		return
	}
	d := info.Disks[i]
	last := lastSample(hist)
	var health *hwmon.DiskHealth
	var sata *hwmon.SATAHealth
	if last != nil && i < len(last.Disks) {
		health = last.Disks[i].Health
		sata = last.Disks[i].SATA
	}
	// Health is read from the drive itself: the NVMe log or the ATA SMART
	// table. Only SSDs (and only those that answer) get a panel.
	if d.Kind == "SSD" || health != nil || sata != nil {
		m.hwHealthPanel(c, t, health, sata)
		if health != nil {
			m.hwStatGrid(c, t, healthStats(health))
		}
		if sata != nil {
			m.hwStatGrid(c, t, sataStats(sata))
		}
	}
	active := hwLine{col: hwDiskCol, fill: true, val: func(s *hwmon.Sample) (float64, bool) {
		if i < len(s.Disks) {
			return s.Disks[i].Active, true
		}
		return 0, false
	}}
	m.hwChartPanel(c, t, hist, "Active time", "100%", chartOpts{max: 100, col: hwDiskCol, lines: []hwLine{active}}, 160, hwLegend{"Active", hwDiskCol})
	rate := func(read bool) hwLine {
		wcol := hwDiskCol.Mix(ui.Hex("#3b82f6"), 0.7)
		l := hwLine{col: hwDiskCol, fill: read}
		if !read {
			l.col = wcol
		}
		l.val = func(s *hwmon.Sample) (float64, bool) {
			if i >= len(s.Disks) {
				return 0, false
			}
			if read {
				return float64(s.Disks[i].ReadRate), true
			}
			return float64(s.Disks[i].WriteRate), true
		}
		return l
	}
	o := chartOpts{floor: 1 << 20, col: hwDiskCol, lines: []hwLine{rate(true), rate(false)}}
	m.hwChartPanel(c, t, hist, "Transfer rate", format.Rate(uint64(chartScale(hist, o))), o, 160, hwLegend{"Read", hwDiskCol}, hwLegend{"Write", hwDiskCol.Mix(ui.Hex("#3b82f6"), 0.7)})

	stats := [][2]string{{"Active time", "—"}, {"Avg response time", "—"}, {"Read speed", "—"}, {"Write speed", "—"}, {"Queue length", "—"}, {"Read since boot", "—"}, {"Written since boot", "—"}}
	if last != nil && i < len(last.Disks) {
		ds := last.Disks[i]
		stats = [][2]string{{"Active time", pct(ds.Active)}, {"Avg response time", fmt.Sprintf("%.2f ms", ds.RespMs)},
			{"Read speed", format.Rate(ds.ReadRate)}, {"Write speed", format.Rate(ds.WriteRate)}, {"Queue length", fmt.Sprint(ds.Queue)},
			{"Read since boot", format.Bytes(ds.ReadBytes)}, {"Written since boot", format.Bytes(ds.WriteBytes)}}
	}
	m.hwStatGrid(c, t, stats)

	if len(d.Letters) > 0 {
		m.hwPanel(c, t, func() {
			ui.Text(c, "Volumes").Bold().FontSize(13)
			for _, l := range d.Letters {
				for _, v := range m.diskVolumes {
					if !strings.HasPrefix(strings.ToUpper(v.Path), l) {
						continue
					}
					used := float64(v.Total-min(v.Free, v.Total)) / max(1, float64(v.Total))
					ui.Column(c).Gap(4).Children(func() {
						ui.Row(c).Gap(8).Children(func() {
							ui.Text(c, strings.TrimSpace(l+" "+v.Label)).Bold()
							ui.Text(c, v.FS).TextColor(t.TextMuted)
							ui.Spacer(c)
							ui.Text(c, fmt.Sprintf("%s free of %s", format.Bytes(v.Free), format.Bytes(v.Total))).TextColor(t.TextMuted)
						})
						ui.Box(c).FillWidth().Height(8).Draw(func(p *ui.Painter, r ui.Rect) {
							p.Fill(r, hwDiskCol.Alpha(0.16), 4)
							col := hwDiskCol
							if used >= 0.9 {
								col = t.Danger
							}
							p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(used), H: r.H}, col, 4)
						})
					})
				}
			}
		})
	}
	m.hwSpecs(c, t, "Specifications", [][2]string{
		{"Model", d.Model},
		{"Serial number", d.Serial},
		{"Firmware", d.Firmware},
		{"Type", d.Kind},
		{"Interface", d.Bus},
		{"Link", diskLink(d)},
		{"Capacity", format.Bytes(d.Size)},
		{"Partition style", d.PartitionStyle},
		{"Drive letters", strings.Join(d.Letters, "  ")},
		{"Physical drive", fmt.Sprintf(`\\.\PhysicalDrive%d`, d.Index)},
	})
}

// hwHealthPanel is the disk detail's verdict panel: what the drive's own
// health log or SMART table says, with nothing invented on top of it.
func (m *Model) hwHealthPanel(c *ui.Context, t *ui.Theme, health *hwmon.DiskHealth, sata *hwmon.SATAHealth) {
	m.hwPanel(c, t, func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Health").Bold().FontSize(13)
			ui.Spacer(c)
			source := "NVMe SMART / health log"
			if health == nil && sata != nil {
				source = "ATA SMART attributes"
			}
			ui.Text(c, source).FontSize(11).TextColor(t.TextMuted)
		})
		switch {
		case health == nil && sata == nil:
			ui.Text(c, "Health information is not available for this drive.").TextColor(t.TextMuted)
			ui.Text(c, "USB bridges and NVMe drives behind a RAID driver (Intel RST/VMD) do not pass the drive's health log through. Nothing is assumed about the drive's condition.").FontSize(12).TextColor(t.TextMuted)
		case health != nil:
			status, reasons := hwmon.JudgeHealth(health)
			m.hwVerdict(c, t, status, reasons)
		default:
			status, reasons := hwmon.JudgeSATAHealth(sata)
			m.hwVerdict(c, t, status, reasons)
		}
	})
}

func (m *Model) hwVerdict(c *ui.Context, t *ui.Theme, status string, reasons []string) {
	col := hwHealthColor(status, t)
	ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(10, 10).Radius(5).Background(col)
		ui.Text(c, status).FontSize(16).Bold().TextColor(col)
		ui.Text(c, "judged from the drive's own counters").FontSize(11).TextColor(t.TextMuted)
	})
	if len(reasons) == 0 {
		ui.Text(c, "No faults reported by the drive's own counters.").FontSize(12).TextColor(t.TextMuted)
	} else {
		for _, r := range reasons {
			ui.Text(c, "• "+r).FontSize(12).TextColor(col)
		}
	}
}

// sataStats lays out the ATA SMART readings. Wear attributes are shown as
// the drive reports them; their meaning is not standardized.
func sataStats(h *hwmon.SATAHealth) [][2]string {
	items := [][2]string{
		{"Reallocated sectors", format.Int(h.Reallocated)},
		{"Pending sectors", format.Int(h.Pending)},
		{"Uncorrectable sectors", format.Int(h.OfflineUncorrect)},
		{"Reported uncorrectable", format.Int(h.ReportedUncorrect)},
	}
	add := func(k, v string) {
		if v != "" {
			items = append(items, [2]string{k, v})
		}
	}
	if h.Temperature > 0 {
		add("Temperature", fmt.Sprintf("%d °C", h.Temperature))
	}
	if h.PowerOnHours > 0 {
		add("Power-on time", durHM(time.Duration(h.PowerOnHours)*time.Hour))
	}
	if h.PowerCycles > 0 {
		add("Power cycles", format.Int(h.PowerCycles))
	}
	add("Interface CRC errors", format.Int(h.UDMACRC))
	if h.LifeLeft > 0 {
		add("Life left", fmt.Sprintf("%d%%", h.LifeLeft))
	}
	if h.Wearout > 0 {
		add("Endurance remaining", fmt.Sprintf("%d%%", h.Wearout))
	}
	if h.TotalWritten > 0 {
		add("Total written", format.Int(h.TotalWritten)+" sectors")
	}
	if h.TotalRead > 0 {
		add("Total read", format.Int(h.TotalRead)+" sectors")
	}
	return items
}

// healthStats are the raw figures behind the verdict. The counters the NVMe
// spec does not treat as faults — unsafe shutdowns, error log entries — are
// shown as information only and never fold into the verdict.
func healthStats(h *hwmon.DiskHealth) [][2]string {
	temp := "—"
	if h.TempC > 0 {
		temp = fmt.Sprintf("%d °C", h.TempC)
	}
	spare := fmt.Sprintf("%d%%", h.AvailableSpare)
	if h.AvailableSpareThreshold > 0 {
		spare = fmt.Sprintf("%d%% (threshold %d%%)", h.AvailableSpare, h.AvailableSpareThreshold)
	}
	items := [][2]string{
		{"Temperature", temp},
		{"Endurance used", fmt.Sprintf("%d%%", h.PercentageUsed)},
		{"Available spare", spare},
		{"Data written", format.Bytes(h.DataWritten)},
		{"Data read", format.Bytes(h.DataRead)},
		{"Power-on time", durHM(time.Duration(h.PowerOnHours) * time.Hour)},
		{"Power cycles", format.Int(h.PowerCycles)},
		{"Unsafe shutdowns", format.Int(h.UnsafeShutdowns)},
		{"Media errors", format.Int(h.MediaErrors)},
		{"Error log entries", format.Int(h.ErrorLogEntries)},
	}
	if h.WarningTempMinutes > 0 || h.CriticalTempMinutes > 0 {
		items = append(items,
			[2]string{"Above warning temp", durHM(time.Duration(h.WarningTempMinutes) * time.Minute)},
			[2]string{"Above critical temp", durHM(time.Duration(h.CriticalTempMinutes) * time.Minute)})
	}
	return items
}

func (m *Model) hwNetDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample, cd *hwCard, index uint32) {
	k := slices.IndexFunc(info.NICs, func(n hwmon.NICInfo) bool { return n.Index == index })
	if k < 0 {
		return
	}
	n := info.NICs[k]
	last := lastSample(hist)
	upCol := hwNetCol.Mix(ui.Hex("#ef4444"), 0.55)
	m.hwChartPanel(c, t, hist, "Throughput", format.Rate(uint64(chartScale(hist, cd.opts))), cd.opts, 200, hwLegend{"Download", hwNetCol}, hwLegend{"Upload", upCol})
	stats := [][2]string{{"Download", "—"}, {"Upload", "—"}, {"Received", "—"}, {"Sent", "—"}, {"Link speed", "—"}}
	if ns := nicSample(last, index); ns != nil {
		stats = [][2]string{{"Download", format.Rate(ns.InRate)}, {"Upload", format.Rate(ns.OutRate)},
			{"Received", format.Bytes(ns.InTotal)}, {"Sent", format.Bytes(ns.OutTotal)}, {"Link speed", cmp.Or(linkSpeed(ns.LinkBps), "—")},
			{"Errors", format.Int(ns.InErrors + ns.OutErrors)}, {"Dropped", format.Int(ns.InDiscards + ns.OutDiscards)}}
	}
	m.hwStatGrid(c, t, stats)
	m.hwSpecs(c, t, "Specifications", [][2]string{
		{"Adapter", n.Name},
		{"Description", deTM(n.Description)},
		{"Type", n.Kind},
		{"MAC address", n.MAC},
		{"IP addresses", strings.Join(n.IPs, "\n")},
		{"MTU", fmt.Sprint(n.MTU)},
	})
}

func (m *Model) hwBatteryDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, hist []hwmon.Sample, cd *hwCard) {
	m.hwChartPanel(c, t, hist, "Charge level", "100%", cd.opts, 200, hwLegend{"Charge", hwBatCol})
	stats := [][2]string{{"Charge", "—"}, {"State", "—"}, {"Time remaining", "—"}}
	if last := lastSample(hist); last != nil && last.Battery != nil {
		b := last.Battery
		remain := "—"
		if b.Remain > 0 {
			remain = durHM(b.Remain)
		}
		stats = [][2]string{{"Charge", fmt.Sprintf("%d%%", b.Percent)}, {"State", batteryState(b)}, {"Time remaining", remain}}
	}
	m.hwStatGrid(c, t, stats)
	// The pack's own identity: wear comes from the firmware, not from the
	// charge percentage.
	if b := info.Batt; b != nil {
		health, cycles := "", ""
		if b.DesignCapacity > 0 && b.FullCapacity > 0 {
			health = fmt.Sprintf("%.0f%%", float64(b.FullCapacity)/float64(b.DesignCapacity)*100)
		}
		if b.CycleCount > 0 {
			cycles = format.Int(b.CycleCount)
		}
		m.hwSpecs(c, t, "Battery", [][2]string{
			{"Design capacity", capWh(b.DesignCapacity)},
			{"Full charge capacity", capWh(b.FullCapacity)},
			{"Health", health},
			{"Cycle count", cycles},
			{"Chemistry", b.Chemistry},
		})
	}
}

// hwDisplayDetail shows what the monitor itself reports over its link
// (EDID), plus the mode the output is running now.
func (m *Model) hwDisplayDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info, i int) {
	if i >= len(info.Displays) {
		return
	}
	d := info.Displays[i]
	native := ""
	if d.NativeWidth > 0 {
		native = fmt.Sprintf("%d × %d", d.NativeWidth, d.NativeHeight)
	}
	now := ""
	if info.DisplayMode.Width > 0 {
		now = fmt.Sprintf("%d × %d @ %d Hz, %d-bit", info.DisplayMode.Width, info.DisplayMode.Height, info.DisplayMode.RefreshHz, info.DisplayMode.BitsPerPel)
	}
	year := ""
	if d.Year > 0 {
		year = fmt.Sprint(d.Year)
	}
	m.hwSpecs(c, t, "Monitor", [][2]string{
		{"Manufacturer", d.Manufacturer},
		{"Model", d.Model},
		{"Serial number", d.Serial},
		{"Screen size", sizeInches(d.WidthCM, d.HeightCM)},
		{"Panel dimensions", cmPair(d.WidthCM, d.HeightCM)},
		{"Native resolution", native},
		{"Current mode", now},
		{"Year of manufacture", year},
	})
	m.hwPanel(c, t, func() {
		ui.Text(c, "Where this comes from").Bold().FontSize(13)
		ui.Text(c, "Decoded from the monitor's EDID — the block the display reports over its link — and from the current display mode. Only monitors actually attached are listed; Windows keeps EDID for displays that have been unplugged.").FontSize(12).TextColor(t.TextMuted)
	})
}

// hwAudioDetail lists the audio adapters SetupAPI reports.
func (m *Model) hwAudioDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info) {
	if len(info.Audio) == 0 {
		return
	}
	m.hwPanel(c, t, func() {
		ui.Text(c, "Audio devices").Bold().FontSize(13)
		ui.Grid(c).ColumnTracks(ui.Fr(3), ui.Fr(2)).GapX(16).GapY(8).Children(func() {
			for _, a := range info.Audio {
				ui.Text(c, a.Name).Selectable().SingleLine().Ellipsis("…")
				ui.Text(c, cmp.Or(a.Vendor, "—")).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
			}
		})
	})
}

// hwDevicesDetail lists every present Plug-and-Play device as one
// collapsible group per class, with each device's running state.
func (m *Model) hwDevicesDetail(c *ui.Context, t *ui.Theme, info *hwmon.Info) {
	if len(info.Devices) == 0 {
		return
	}
	if m.hw.devOpen == nil {
		m.hw.devOpen = map[string]bool{}
	}
	var classes []string
	groups := map[string][]hwmon.DeviceInfo{}
	for _, d := range info.Devices {
		if _, ok := groups[d.Class]; !ok {
			classes = append(classes, d.Class)
		}
		groups[d.Class] = append(groups[d.Class], d)
	}
	m.hwPanel(c, t, func() {
		ui.Row(c).Children(func() {
			ui.Text(c, "Present devices").Bold().FontSize(13)
			ui.Spacer(c)
			ui.Text(c, fmt.Sprintf("%d devices in %d classes", len(info.Devices), len(classes))).FontSize(11).TextColor(t.TextMuted)
		})
		ui.Accordion(c, func() {
			for _, class := range classes {
				open := m.hw.devOpen[class]
				devs := groups[class]
				if ui.AccordionItem(c, fmt.Sprintf("%s · %d", class, len(devs)), &open, func() {
					ui.Grid(c).ColumnTracks(ui.Fr(2.6), ui.Fr(1.8), ui.Fr(1)).GapX(12).GapY(6).Padding(6, 4).Children(func() {
						for _, d := range devs {
							ui.Text(c, d.Name).SingleLine().Ellipsis("…")
							ui.Text(c, cmp.Or(d.Vendor, "—")).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
							ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Box(c).Size(7, 7).Radius(4).Background(devStatusColor(d, t))
								ui.Text(c, devStatusText(d)).FontSize(11).TextColor(t.TextMuted).SingleLine()
							})
						}
					})
				}).Changed() {
					m.hw.devOpen[class] = open
				}
			}
		})
	})
}

// devStatusColor marks a device green when its driver is running,
// orange when it reports a problem and muted when it sits idle.
func devStatusColor(d hwmon.DeviceInfo, t *ui.Theme) ui.Color {
	switch {
	case d.Problem != 0:
		return t.Warning
	case d.Started:
		return t.Success
	default:
		return t.TextMuted
	}
}

func devStatusText(d hwmon.DeviceInfo) string {
	switch {
	case d.Problem != 0:
		return fmt.Sprintf("Problem %d", d.Problem)
	case d.Started:
		return "Working"
	default:
		return "Idle"
	}
}
