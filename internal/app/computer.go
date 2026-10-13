package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dumpingground/dumpingground/internal/format"
	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo/ui"
)

// Series colors, one hue per kind of hardware; they hold up on both the
// light and the dark theme.
var (
	hwCPUCol  = ui.Hex("#3b82f6")
	hwMemCol  = ui.Hex("#a855f7")
	hwGPUCol  = ui.Hex("#14b8a6")
	hwDiskCol = ui.Hex("#22c55e")
	hwNetCol  = ui.Hex("#f59e0b")
	hwBatCol  = ui.Hex("#84cc16")
	hwDispCol = ui.Hex("#0ea5e9")
	hwAudCol  = ui.Hex("#ec4899")
	hwDevCol  = ui.Hex("#64748b")
)

// computerState is the Computer page's model: the hardware inventory, a
// rolling sample history and which card (if any) is open.
//
// The sampler goroutine owns writes to hist; the view reads a per-frame
// copy. Sampling runs whenever the window is not minimized — it costs a
// handful of syscalls — but only asks for the heavier GPU query, and only
// repaints, while the page is on screen.
type computerState struct {
	mon     *hwmon.Monitor
	info    atomic.Pointer[hwmon.Info]
	mu      sync.Mutex
	hist    []hwmon.Sample
	frame   []hwmon.Sample  // view-thread copy of hist
	detail  string          // id of the open card, "" for the overview
	visible atomic.Bool     // the Computer page is on screen
	running atomic.Bool     // the window is not minimized
	railW   float32         // catalogue width the user dragged it to, 0 = default
	devOpen map[string]bool // device-class groups the user expanded
	grid    ui.GridState
}

// startHardware loads the inventory and then samples once a second until
// the app closes. Load takes tens of milliseconds, so it runs here, not on
// the UI thread.
func (m *Model) startHardware() {
	m.hw.mon = hwmon.New()
	m.hw.running.Store(true)
	go func() {
		m.hw.info.Store(m.hw.mon.Load())
		m.hw.mon.Sample(false) // baseline for the rate counters; its zeros are not data
		tk := time.NewTicker(time.Second)
		defer tk.Stop()
		nicCheck := time.Now()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-tk.C:
			}
			if !m.hw.running.Load() {
				continue
			}
			if time.Since(nicCheck) > 5*time.Second {
				nicCheck = time.Now()
				if fresh := m.hw.mon.RefreshNICs(); fresh != nil {
					m.hw.info.Store(fresh)
				}
			}
			vis := m.hw.visible.Load()
			// A GPU tray icon needs the utilization counter even while
			// the Computer page is hidden.
			s := m.hw.mon.Sample(vis || m.trayMetric == "GPU")
			m.hw.mu.Lock()
			m.hw.hist = append(m.hw.hist, s)
			if len(m.hw.hist) > hwKeep {
				m.hw.hist = append(m.hw.hist[:0], m.hw.hist[len(m.hw.hist)-hwKeep:]...)
			}
			m.hw.mu.Unlock()
			m.updateTrayIcon(s)
			if vis && m.window != nil {
				m.window.Invalidate()
			}
		}
	}()
}

// hwSnapshot copies the history for this frame.
func (m *Model) hwSnapshot() []hwmon.Sample {
	m.hw.mu.Lock()
	m.hw.frame = append(m.hw.frame[:0], m.hw.hist...)
	m.hw.mu.Unlock()
	return m.hw.frame
}

// hwCard is one hardware card: what the overview draws and what the detail
// page opens. Every field is derived each frame from info and the history.
type hwCard struct {
	id, glyph   string
	title, sub  string
	col         ui.Color
	value, vsub string // the big figure, and a smaller line under it
	health      string // disks: "Good", "Caution", "Replace"; "" when unknown
	opts        chartOpts
}

// hwCards builds the catalogue's cards in display order: the machine
// itself first, then CPU, memory, each GPU, each disk, each network
// adapter, and the battery when there is one.
func hwCards(info *hwmon.Info, h []hwmon.Sample) []hwCard {
	var last *hwmon.Sample
	if len(h) > 0 {
		last = &h[len(h)-1]
	}
	var cards []hwCard

	mc := hwCard{id: "machine", glyph: "monitor", title: cmp.Or(info.Host, tr("Computer")), sub: strings.TrimSpace(info.Manufacturer + " " + info.Model), col: hwCPUCol}
	if info.Battery {
		mc.glyph = "laptop"
	}
	if last != nil {
		mc.value = fmt.Sprintf(tr("Up %s"), durHM(last.Uptime))
	}
	cards = append(cards, mc)

	cpu := hwCard{id: "cpu", glyph: "cpu", title: tr("CPU"), sub: shortCPU(info.CPU.Name), col: hwCPUCol, value: "—",
		opts: chartOpts{max: 100, col: hwCPUCol, lines: []hwLine{{val: func(s *hwmon.Sample) (float64, bool) { return s.CPU, true }, col: hwCPUCol, fill: true}}}}
	if last != nil {
		cpu.value = fmt.Sprintf("%.0f%%", last.CPU)
	}
	cards = append(cards, cpu)

	mem := hwCard{id: "mem", glyph: "mem", title: tr("Memory"), sub: memSummary(info), col: hwMemCol, value: "—",
		opts: chartOpts{max: 100, col: hwMemCol, lines: []hwLine{{val: func(s *hwmon.Sample) (float64, bool) { return memPct(s), s.MemTotal > 0 }, col: hwMemCol, fill: true}}}}
	if last != nil {
		mem.value = fmt.Sprintf("%.0f%%", memPct(last))
	}
	cards = append(cards, mem)

	for i, g := range info.GPUs {
		i := i
		title := tr("GPU")
		if len(info.GPUs) > 1 {
			title = fmt.Sprintf(tr("GPU %d"), i)
		}
		c := hwCard{id: fmt.Sprintf("gpu:%d", i), glyph: "gpu", title: title, sub: deTM(g.Name), col: hwGPUCol, value: "—",
			opts: chartOpts{max: 100, col: hwGPUCol, lines: []hwLine{{val: func(s *hwmon.Sample) (float64, bool) {
				if i < len(s.GPUs) && s.GPUs[i].HasUtilization {
					return s.GPUs[i].Util, true
				}
				return 0, false
			}, col: hwGPUCol, fill: true}}}}
		if last != nil && i < len(last.GPUs) {
			gs := last.GPUs[i]
			if gs.HasUtilization {
				c.value = fmt.Sprintf("%.0f%%", gs.Util)
			}
			if gs.HasMemory {
				c.vsub = gpuMemLine(g, gs)
			}
		}
		cards = append(cards, c)
	}

	for i, d := range info.Disks {
		i := i
		title := fmt.Sprintf(tr("Disk %d"), d.Index)
		if len(d.Letters) > 0 {
			title += " (" + strings.Join(d.Letters, " ") + ")"
		}
		c := hwCard{id: fmt.Sprintf("disk:%d", i), glyph: "hdd", title: title, sub: strings.TrimSpace(d.Model + " · " + strings.TrimSpace(d.Kind+" "+d.Bus)), col: hwDiskCol, value: "—",
			opts: chartOpts{max: 100, col: hwDiskCol, lines: []hwLine{{val: func(s *hwmon.Sample) (float64, bool) {
				if i < len(s.Disks) {
					return s.Disks[i].Active, true
				}
				return 0, false
			}, col: hwDiskCol, fill: true}}}}
		if last != nil && i < len(last.Disks) {
			ds := last.Disks[i]
			c.value = fmt.Sprintf("%.0f%%", ds.Active)
			c.vsub = fmt.Sprintf(tr("R %s · W %s"), format.Rate(ds.ReadRate), format.Rate(ds.WriteRate))
			switch {
			case ds.Health != nil:
				c.health, _ = hwmon.JudgeHealth(ds.Health)
			case ds.SATA != nil:
				c.health, _ = hwmon.JudgeSATAHealth(ds.SATA)
			}
		}
		cards = append(cards, c)
	}

	for _, n := range info.NICs {
		c := hwCard{id: fmt.Sprintf("net:%d", n.Index), glyph: nicGlyph(n.Kind), title: n.Name, sub: deTM(n.Description), col: hwNetCol, value: "—",
			opts: netChart(n.Index)}
		if ns := nicSample(last, n.Index); ns != nil {
			c.value, c.vsub = "↓ "+format.Rate(ns.InRate), "↑ "+format.Rate(ns.OutRate)
			if !ns.Up {
				c.value, c.vsub = tr("Not connected"), ""
			}
		}
		cards = append(cards, c)
	}

	if info.Battery {
		c := hwCard{id: "battery", glyph: "battery", title: tr("Battery"), col: hwBatCol, value: "—",
			opts: chartOpts{max: 100, col: hwBatCol, lines: []hwLine{{val: func(s *hwmon.Sample) (float64, bool) {
				if s.Battery != nil && s.Battery.Percent >= 0 {
					return float64(s.Battery.Percent), true
				}
				return 0, false
			}, col: hwBatCol, fill: true}}}}
		if last != nil && last.Battery != nil {
			c.value = fmt.Sprintf("%d%%", last.Battery.Percent)
			c.sub = batteryState(last.Battery)
		}
		cards = append(cards, c)
	}

	// The static cards: monitors (from EDID), audio and the present-device
	// list. They have no live series, so their card bodies are plain rows.
	for i, d := range info.Displays {
		i := i
		title := tr("Display")
		if len(info.Displays) > 1 {
			title = fmt.Sprintf(tr("Display %d"), i+1)
		}
		c := hwCard{id: fmt.Sprintf("display:%d", i), glyph: "monitor", title: title,
			sub: deTM(d.Manufacturer + " " + d.Model), col: hwDispCol, value: fmt.Sprintf("%d×%d", d.NativeWidth, d.NativeHeight)}
		cards = append(cards, c)
	}

	if len(info.Audio) > 0 {
		c := hwCard{id: "audio", glyph: "audio", title: tr("Audio"), sub: tr("adapters and endpoints"), col: hwAudCol, value: fmt.Sprint(len(info.Audio))}
		cards = append(cards, c)
	}

	if len(info.Devices) > 0 {
		c := hwCard{id: "devices", glyph: "chip", title: tr("Devices"), sub: tr("present hardware"), col: hwDevCol, value: fmt.Sprint(len(info.Devices))}
		counts := map[string]int{}
		for _, d := range info.Devices {
			counts[d.Class]++
		}
		var top string
		best := 0
		for k, n := range counts {
			if n > best || (n == best && k < top) {
				top, best = k, n
			}
		}
		if top != "" {
			c.sub = fmt.Sprintf(tr("%s ×%d, %d classes"), top, best, len(counts))
		}
		cards = append(cards, c)
	}
	return cards
}

// nicSample finds an adapter's reading by interface index: the adapter set
// changes while the app runs, so slice positions are not stable across the
// history.
func nicSample(s *hwmon.Sample, index uint32) *hwmon.NICSample {
	if s == nil {
		return nil
	}
	for i := range s.NICs {
		if s.NICs[i].Index == index {
			return &s.NICs[i]
		}
	}
	return nil
}

// netChart plots download and upload on one auto-scaling axis (at least
// 100 KB/s tall, so idle chatter does not fill the chart).
func netChart(index uint32) chartOpts {
	rate := func(down bool) func(*hwmon.Sample) (float64, bool) {
		return func(s *hwmon.Sample) (float64, bool) {
			ns := nicSample(s, index)
			if ns == nil {
				return 0, false
			}
			if down {
				return float64(ns.InRate), true
			}
			return float64(ns.OutRate), true
		}
	}
	return chartOpts{floor: 100 << 10, col: hwNetCol, lines: []hwLine{
		{val: rate(true), col: hwNetCol, fill: true},
		{val: rate(false), col: hwNetCol.Mix(ui.Hex("#ef4444"), 0.55)},
	}}
}

// hwHealthColor maps a health verdict to the theme's status color.
func hwHealthColor(status string, t *ui.Theme) ui.Color {
	switch status {
	case "Replace":
		return t.Danger
	case "Caution":
		return t.Warning
	default:
		return t.Success
	}
}

func memPct(s *hwmon.Sample) float64 {
	if s.MemTotal == 0 {
		return 0
	}
	return float64(s.MemUsed) / float64(s.MemTotal) * 100
}

func nicGlyph(kind string) string {
	if kind == "Wi-Fi" {
		return "wifi"
	}
	return "globe"
}

// deTM drops the (R)/(TM) marks vendors embed in device names and
// collapses the padding around them.
func deTM(n string) string {
	n = strings.NewReplacer("(R)", "", "(TM)", "", "(tm)", "", "(r)", "").Replace(n)
	return strings.Join(strings.Fields(n), " ")
}

// shortCPU also trims the clock suffix from a processor name:
// "Intel(R) Core(TM) i7-13700K CPU @ 3.40GHz" → "Intel Core i7-13700K".
func shortCPU(n string) string {
	n = deTM(strings.NewReplacer(" CPU", "", " Processor", "").Replace(n))
	if i := strings.Index(n, "@"); i > 0 {
		n = strings.TrimSpace(n[:i])
	}
	return n
}

// memSummary reads "64 GB · LPDDR5 7467 MT/s · 8 × 8 GB" off the SMBIOS
// module list, falling back to the OS-visible total when the firmware
// gave none.
func memSummary(info *hwmon.Info) string {
	if len(info.Mem.Modules) == 0 {
		return format.Bytes(info.Mem.Total)
	}
	var total uint64
	for _, d := range info.Mem.Modules {
		total += d.Size
	}
	s := fmt.Sprintf("%d GB", (total+(1<<29))>>30)
	d := info.Mem.Modules[0]
	speed := d.ConfigMTs
	if speed == 0 {
		speed = d.SpeedMTs
	}
	if d.Type != "" {
		s += " · " + d.Type
		if speed > 0 {
			s += fmt.Sprintf(" %d MT/s", speed)
		}
	}
	if cfg := memConfig(info.Mem.Modules); cfg != "" {
		s += " · " + cfg
	}
	return s
}

// memConfig condenses the DIMM list to "8 × 8 GB" — "2 × 16 GB + 2 × 8 GB"
// when capacities are mixed — so a 64 GB total reads as one or several
// modules at a glance.
func memConfig(mods []hwmon.DIMM) string {
	if len(mods) == 0 {
		return ""
	}
	counts := map[uint64]int{}
	var sizes []uint64
	for _, d := range mods {
		if counts[d.Size] == 0 {
			sizes = append(sizes, d.Size)
		}
		counts[d.Size]++
	}
	slices.SortFunc(sizes, func(a, b uint64) int { return cmp.Compare(b, a) })
	var parts []string
	for _, sz := range sizes {
		parts = append(parts, fmt.Sprintf("%d × %s", counts[sz], format.Bytes(sz)))
	}
	return strings.Join(parts, " + ")
}

// diskLink renders the PCI link the drive hangs off: the slot's top
// generation and width, with the power-managed current negotiation noted
// when it sits lower.
func diskLink(d hwmon.DiskInfo) string {
	gen, lanes := d.PCIeGenMax, d.PCIeLanesMax
	if gen == 0 {
		gen, lanes = d.PCIeGen, d.PCIeLanes
	}
	if gen == 0 {
		return ""
	}
	s := fmt.Sprintf("PCIe %d.0", gen)
	if lanes > 0 {
		s += fmt.Sprintf(" ×%d", lanes)
	}
	if d.PCIeGen > 0 && d.PCIeGen < gen {
		s += fmt.Sprintf(tr(" (link now at Gen%d)"), d.PCIeGen)
	}
	return s
}

// gpuMemLine reports VRAM for a discrete card and shared memory for an
// integrated one, which has little dedicated memory of its own.
func gpuMemLine(g hwmon.GPUInfo, s hwmon.GPUSample) string {
	if g.DedicatedBytes >= 1<<30 {
		return fmt.Sprintf(tr("VRAM %s / %s"), format.Bytes(s.DedicatedUsed), format.Bytes(g.DedicatedBytes))
	}
	return tr("Shared memory") + " " + format.Bytes(s.SharedUsed+s.DedicatedUsed)
}

func linkSpeed(bps uint64) string {
	switch {
	case bps == 0 || bps == ^uint64(0):
		return ""
	case bps >= 1e9:
		return fmt.Sprintf("%.1f Gbps", float64(bps)/1e9)
	}
	return fmt.Sprintf("%d Mbps", bps/1000000)
}

func batteryState(b *hwmon.BatterySample) string {
	switch {
	case b.Charging:
		return tr("Charging")
	case b.OnAC:
		return tr("Plugged in")
	case b.Remain > 0:
		return fmt.Sprintf(tr("On battery · %s left"), durHM(b.Remain))
	}
	return tr("On battery")
}

// durHM renders "2 h 10 min" style spans.
func durHM(d time.Duration) string {
	d = d.Round(time.Minute)
	days, h, m := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0:
		return fmt.Sprintf(tr("%d d %d h %d min"), days, h, m)
	case h > 0:
		return fmt.Sprintf(tr("%d h %d min"), h, m)
	}
	return fmt.Sprintf(tr("%d min"), m)
}
