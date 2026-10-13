package app

import (
	"fmt"

	"github.com/dumpingground/dumpingground/internal/appdir"
	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo/ui"
)

// version tracks the app version; bump it together with mygo.json at
// release time.
const version = "0.4.0"

// settingsView is the settings window: a large modal with a borderless
// catalogue rail on the left, in the same style as the Computer page,
// and the selected section's controls on the right.
func (m *Model) settingsView(c *ui.Context) {
	t := c.Theme()
	ui.Modal(c, &m.settingsOpen, func() {
		ui.Column(c).Width(760).Height(500).Radius(12).Background(t.Background).Children(func() {
			ui.Row(c).Padding(16, 16, 4, 16).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("Settings")).Bold().FontSize(15)
				ui.Spacer(c)
				if m.iconButton(c, "x", tr("Close")).Clicked() {
					m.settingsOpen = false
				}
			})
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				m.settingsRail(c, t)
				ui.Scroll(c.Key("settings:detail")).Grow(1).MinHeight(0).Children(func() {
					ui.Column(c).Padding(6, 20, 16, 20).Gap(6).AlignItems(ui.Stretch).Children(func() {
						switch m.settingsSection {
						case "scanning":
							m.settingsScanning(c, t)
						case "tray":
							m.settingsTray(c, t)
						case "about":
							m.settingsAbout(c, t)
						default:
							m.settingsGeneral(c, t)
						}
					})
				})
			})
		})
	})
}

// settingsSection is one catalogue entry: icon, label and the pane it
// opens.
type settingsItem struct {
	id, glyph, label string
}

var settingsItems = []settingsItem{
	{id: "general", glyph: "monitor", label: "Display & language"},
	{id: "scanning", glyph: "search", label: "Processes & scanning"},
	{id: "tray", glyph: "chart", label: "Tray & window"},
	{id: "about", glyph: "info", label: "About"},
}

func (m *Model) settingsRail(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(190).Shrink(0).Padding(10, 8).Gap(2).Children(func() {
		for _, it := range settingsItems {
			it := it
			label := tr(it.label)
			on := m.settingsSection == it.id
			e := ui.Row(c.Key("set:" + it.id)).Height(36).PaddingX(12).Gap(10).AlignItems(ui.Center).Radius(7).Cursor(ui.CursorPointer).Label(label)
			e.Draw(func(p *ui.Painter, r ui.Rect) {
				switch {
				case on:
					p.Fill(r, t.Accent.Alpha(0.12), 7)
					p.Fill(ui.Rect{X: r.X + 3, Y: r.Y + 8, W: 3, H: r.H - 16}, t.Accent, 2)
				case e.Hovered():
					p.Fill(r, t.SurfaceHover, 7)
				}
			})
			e.Children(func() {
				ic := icon(c, it.glyph, 16)
				tx := ui.Text(c, label).FontSize(12).SingleLine()
				if on {
					ic.TextColor(t.Accent)
					tx.Bold()
				} else {
					ic.TextColor(t.TextMuted)
					tx.TextColor(t.TextMuted)
				}
			})
			if e.Clicked() {
				m.settingsSection = it.id
			}
		}
	})
}

// settingsRow is one labelled control line: title and a muted
// explanation on the left, the control on the right.
func (m *Model) settingsRow(c *ui.Context, t *ui.Theme, title, note string, control func()) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).PaddingY(4).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, title)
			if note != "" {
				ui.Text(c, note).FontSize(11).TextColor(t.TextMuted)
			}
		})
		control()
	})
}

// settingsSelectW keeps every dropdown in the window one width.
const settingsSelectW = 150

func (m *Model) settingsGeneral(c *ui.Context, t *ui.Theme) {
	// Theme is stored as an int; bridge it through a local Select.
	themeSel := []string{"System", "Light", "Dark"}[min(2, max(0, m.themeMode))]
	m.settingsRow(c, t, tr("Theme"), tr("Light, dark, or follow Windows"), func() {
		trSelect(c, themeSel, []string{"System", "Light", "Dark"}, func(v string) {
			m.themeMode = map[string]int{"System": 0, "Light": 1, "Dark": 2}[v]
			m.saveSettings()
		}).Width(settingsSelectW)
	})
	m.settingsRow(c, t, tr("Tab style"), tr("Look of the page tabs in the top bar"), func() {
		trSelect(c, m.tabStyle, []string{"Pill", "Underline"}, func(v string) {
			m.tabStyle = v
			m.saveSettings()
		}).Width(settingsSelectW)
	})
	m.settingsRow(c, t, tr("Computer page"), tr("Sidebar catalogue, or the earlier card grid"), func() {
		trSelect(c, m.computerLayout, []string{"Sidebar", "Cards"}, func(v string) {
			m.computerLayout = v
			m.saveSettings()
		}).Width(settingsSelectW)
	})
	m.settingsRow(c, t, tr("Language"), tr("Interface language — applied immediately"), func() {
		trSelect(c, m.language, langOptions, func(v string) {
			m.language = v
			m.applyLanguage()
			m.saveSettings()
		}).Width(settingsSelectW)
	})
}

func (m *Model) settingsScanning(c *ui.Context, t *ui.Theme) {
	m.settingsRow(c, t, tr("Process refresh"), tr("Automatic sampling on the Processes page"), func() {
		trSelect(c, m.processInterval, []string{"1 s", "2 s", "5 s"}, func(v string) {
			m.processInterval = v
			m.applySampleInterval()
			m.saveSettings()
		}).Width(settingsSelectW)
	})
	m.settingsRow(c, t, tr("Space metric"), tr("Used by the space map and percent column"), func() {
		trSelect(c, m.diskMetric, []string{"Allocated", "Logical"}, func(v string) {
			m.diskMetric = v
			m.saveSettings()
		}).Width(settingsSelectW)
	})
	m.settingsRow(c, t, tr("Memory after scan"), tr("Return scan buffers to the OS, or keep them for faster rescans"), func() {
		trSelect(c, m.memRelease, []string{"Automatic", "Keep for rescan"}, func(v string) {
			m.memRelease = v
			m.saveSettings()
		}).Width(settingsSelectW)
	})
	ui.Checkbox(c, &m.animateMap, tr("Animate map drill-down")).Tooltip(tr("Respects Windows reduced motion"))
}

func (m *Model) settingsTray(c *ui.Context, t *ui.Theme) {
	metrics := []string{"CPU", "Memory", "Icon"}
	if info := m.hw.info.Load(); info != nil && hasDiscreteGPU(info) {
		metrics = []string{"CPU", "Memory", "GPU", "Icon"}
	}
	m.settingsRow(c, t, tr("Tray icon shows"), tr("A live sparkline of this metric in the notification area"), func() {
		trSelect(c, m.trayMetric, metrics, func(v string) {
			m.trayMetric = v
			if v == "Icon" && m.tray != nil {
				_ = m.tray.SetIcon(appIcon, false)
				m.tray.SetToolTip("DumpingGround")
			}
			m.saveSettings()
		}).Width(settingsSelectW)
	})
	closeSel := "Quit"
	if m.closeToTray {
		closeSel = "Minimize to tray"
	}
	m.settingsRow(c, t, tr("Close button"), tr("What the window's close button does"), func() {
		trSelect(c, closeSel, []string{"Quit", "Minimize to tray"}, func(v string) {
			m.closeToTray = v == "Minimize to tray"
			m.saveSettings()
		}).Width(settingsSelectW)
	})
	ui.Text(c, tr("Left-click the tray icon to reopen the window; right-click for Open and Quit.")).FontSize(11).TextColor(t.TextMuted).MaxLines(3)
}

func (m *Model) settingsAbout(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).PaddingY(6).Children(func() {
		ui.Box(c).Size(42, 42).Radius(10).Center().Background(t.Accent).Children(func() {
			icon(c, "logo", 24).TextColor(t.AccentText)
		})
		ui.Column(c).Children(func() {
			ui.Text(c, "DumpingGround").Bold().FontSize(16)
			ui.Text(c, fmt.Sprintf(tr("Version %s"), version)).FontSize(11).TextColor(t.TextMuted)
		})
	})
	ui.Text(c, tr("A safe, transparent Windows maintenance tool: process inspection, disk-usage mapping, Rust and Windows junk cleanup, hardware monitoring, software uninstall and startup management.")).FontSize(12).TextColor(t.TextMuted).MaxLines(4)
	ui.Divider(c)
	ui.Text(c, tr("License")).Bold().FontSize(13)
	ui.Text(c, "MIT License").TextColor(t.TextMuted)
	ui.Text(c, "Copyright (c) 2026 DumpingGround contributors").TextColor(t.TextMuted)
	ui.Text(c, mitNotice).FontSize(11).TextColor(t.TextMuted).MaxLines(0)
	ui.Divider(c)
	ui.Text(c, tr("State & logs")).Bold().FontSize(13)
	ui.Text(c, appdir.Dir()).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
}

// hasDiscreteGPU reports whether the machine has a dedicated graphics
// adapter — the tray's GPU sparkline is offered only then.
func hasDiscreteGPU(info *hwmon.Info) bool {
	for _, g := range info.GPUs {
		if g.DedicatedBytes > 0 {
			return true
		}
	}
	return false
}

// mitNotice is the license's grant and warranty disclaimer, shown so a
// copy of the app carries the notice the MIT license asks for.
const mitNotice = `Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.`
