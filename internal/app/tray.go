package app

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"

	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// setupTray adds the notification-area icon. It shows the metric the
// settings picked, opens the window on click and pops Open/Quit on
// right-click.
func (m *Model) setupTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: appIcon, ToolTip: "DumpingGround"})
	if err != nil {
		log.Print("tray: ", err)
		return
	}
	m.tray = tray
	tray.OnClick(m.showWindow)
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: fmt.Sprintf(tr("Open %s"), "DumpingGround"), Click: func(*mygo.MenuItem, *mygo.Window) { m.showWindow() }},
		mygo.Separator(),
		{Label: tr("Quit"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
	tray.OnRightClick(func() {
		tray.SetMenu(menu)
		defer tray.SetMenu(nil)
		tray.PopUpMenu()
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { tray.Destroy() })
}

// showWindow brings the hidden or minimized window back to the front
// and wakes the samplers the hide path paused.
func (m *Model) showWindow() {
	if m.window == nil {
		return
	}
	m.minimized = false
	m.hw.running.Store(true)
	m.samplerActive.Store(m.page == processPage)
	m.window.Show()
	m.window.Restore()
	m.window.Focus()
}

// updateTrayIcon redraws the tray from the latest sample: a sparkline
// of the last stretch of history, with the current figure in the
// tooltip. Called from the hardware sampler goroutine; SetIcon and
// SetToolTip marshal to the main thread themselves.
func (m *Model) updateTrayIcon(s hwmon.Sample) {
	if m.tray == nil {
		return
	}
	if m.trayMetric == "Icon" {
		return // set once from appIcon
	}
	pct, name, col, ok := m.trayValue(s)
	if !ok {
		return
	}
	m.hw.mu.Lock()
	hist := append([]hwmon.Sample(nil), m.hw.hist...)
	m.hw.mu.Unlock()
	if err := m.tray.SetIcon(trayIconPNG(hist, m.trayMetric, col), false); err != nil {
		return
	}
	m.tray.SetToolTip(fmt.Sprintf(tr("DumpingGround — %s %.0f%%"), tr(name), pct))
}

// trayValue resolves the configured metric against a sample.
func (m *Model) trayValue(s hwmon.Sample) (pct float64, name string, col ui.Color, ok bool) {
	switch m.trayMetric {
	case "Memory":
		if s.MemTotal == 0 {
			return 0, "", col, false
		}
		return memPct(&s), "Memory", hwMemCol, true
	case "GPU":
		best := -1.0
		for _, g := range s.GPUs {
			if g.HasUtilization && g.Util > best {
				best = g.Util
			}
		}
		if best < 0 {
			return 0, "", col, false
		}
		return best, "GPU", hwGPUCol, true
	default:
		return s.CPU, "CPU", hwCPUCol, true
	}
}

// traySeries pulls the curve for one metric out of a history sample.
func traySeries(s *hwmon.Sample, metric string) (float64, bool) {
	switch metric {
	case "Memory":
		return memPct(s), s.MemTotal > 0
	case "GPU":
		best := -1.0
		for _, g := range s.GPUs {
			if g.HasUtilization && g.Util > best {
				best = g.Util
			}
		}
		return best, best >= 0
	default:
		return s.CPU, true
	}
}

// trayIconPNG draws the metric sparkline as a 32×32 PNG — the same
// filled-curve look as the Computer page thumbnails, in the metric's
// color on a transparent background.
func trayIconPNG(hist []hwmon.Sample, metric string, col ui.Color) []byte {
	const n = 32
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	ink := color.NRGBA{R: col.R, G: col.G, B: col.B, A: 255}
	fill := color.NRGBA{R: col.R, G: col.G, B: col.B, A: 64}
	if len(hist) > 60 {
		hist = hist[len(hist)-60:]
	}
	prev := -1
	for x := 0; x < n; x++ {
		var v float64
		if len(hist) > 0 {
			if s, ok := traySeries(&hist[x*len(hist)/n], metric); ok {
				v = s
			}
		}
		top := n - 2 - int(v/100*float64(n-4))
		for y := top + 1; y < n-1; y++ {
			img.SetNRGBA(x, y, fill)
		}
		// Stroke the top edge, bridging the previous column so sloped
		// segments stay a continuous line.
		lo, hi := top, top
		if prev >= 0 {
			lo, hi = min(prev, top), max(prev, top)
		}
		for y := lo; y <= hi; y++ {
			img.SetNRGBA(x, y, ink)
		}
		prev = top
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
