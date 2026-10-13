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
				ui.Text(c, "Settings").Bold().FontSize(15)
				ui.Spacer(c)
				if m.iconButton(c, "x", "Close").Clicked() {
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
			on := m.settingsSection == it.id
			e := ui.Row(c.Key("set:" + it.id)).Height(36).PaddingX(12).Gap(10).AlignItems(ui.Center).Radius(7).Cursor(ui.CursorPointer).Label(it.label)
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
				tx := ui.Text(c, it.label).FontSize(12).SingleLine()
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
	m.settingsRow(c, t, "Theme", "Light, dark, or follow Windows", func() {
		if ui.Select(c, &themeSel, []string{"System", "Light", "Dark"}).Width(settingsSelectW).Changed() {
			m.themeMode = map[string]int{"System": 0, "Light": 1, "Dark": 2}[themeSel]
			m.saveSettings()
		}
	})
	m.settingsRow(c, t, "Tab style", "Look of the page tabs in the top bar", func() {
		if ui.Select(c, &m.tabStyle, []string{"Pill", "Underline"}).Width(settingsSelectW).Changed() {
			m.saveSettings()
		}
	})
	m.settingsRow(c, t, "Computer page", "Sidebar catalogue, or the earlier card grid", func() {
		if ui.Select(c, &m.computerLayout, []string{"Sidebar", "Cards"}).Width(settingsSelectW).Changed() {
			m.saveSettings()
		}
	})
	m.settingsRow(c, t, "Language", "Interface language — localization is on the way", func() {
		ui.Select(c, &m.language, []string{"System", "English", "中文"}).Width(settingsSelectW).Disabled(true)
	})
}

func (m *Model) settingsScanning(c *ui.Context, t *ui.Theme) {
	m.settingsRow(c, t, "Process refresh", "Automatic sampling on the Processes page", func() {
		if ui.Select(c, &m.processInterval, []string{"1 s", "2 s", "5 s"}).Width(settingsSelectW).Changed() {
			m.applySampleInterval()
			m.saveSettings()
		}
	})
	m.settingsRow(c, t, "Space metric", "Used by the space map and percent column", func() {
		if ui.Select(c, &m.diskMetric, []string{"Allocated", "Logical"}).Width(settingsSelectW).Changed() {
			m.saveSettings()
		}
	})
	m.settingsRow(c, t, "Memory after scan", "Return scan buffers to the OS, or keep them for faster rescans", func() {
		if ui.Select(c, &m.memRelease, []string{"Automatic", "Keep for rescan"}).Width(settingsSelectW).Changed() {
			m.saveSettings()
		}
	})
	ui.Checkbox(c, &m.animateMap, "Animate map drill-down").Tooltip("Respects Windows reduced motion")
}

func (m *Model) settingsTray(c *ui.Context, t *ui.Theme) {
	metrics := []string{"CPU", "Memory", "Icon"}
	if info := m.hw.info.Load(); info != nil && hasDiscreteGPU(info) {
		metrics = []string{"CPU", "Memory", "GPU", "Icon"}
	}
	m.settingsRow(c, t, "Tray icon shows", "A live sparkline of this metric in the notification area", func() {
		if ui.Select(c, &m.trayMetric, metrics).Width(settingsSelectW).Changed() {
			if m.trayMetric == "Icon" && m.tray != nil {
				_ = m.tray.SetIcon(appIcon, false)
				m.tray.SetToolTip("DumpingGround")
			}
			m.saveSettings()
		}
	})
	closeSel := "Quit"
	if m.closeToTray {
		closeSel = "Minimize to tray"
	}
	m.settingsRow(c, t, "Close button", "What the window's close button does", func() {
		if ui.Select(c, &closeSel, []string{"Quit", "Minimize to tray"}).Width(settingsSelectW).Changed() {
			m.closeToTray = closeSel == "Minimize to tray"
			m.saveSettings()
		}
	})
	ui.Text(c, "Left-click the tray icon to reopen the window; right-click for Open and Quit.").FontSize(11).TextColor(t.TextMuted).MaxLines(3)
}

func (m *Model) settingsAbout(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).PaddingY(6).Children(func() {
		ui.Box(c).Size(42, 42).Radius(10).Center().Background(t.Accent).Children(func() {
			icon(c, "logo", 24).TextColor(t.AccentText)
		})
		ui.Column(c).Children(func() {
			ui.Text(c, "DumpingGround").Bold().FontSize(16)
			ui.Text(c, fmt.Sprintf("Version %s", version)).FontSize(11).TextColor(t.TextMuted)
		})
	})
	ui.Text(c, "A safe, transparent Windows maintenance tool: process inspection, disk-usage mapping, Rust and Windows junk cleanup, hardware monitoring, software uninstall and startup management.").FontSize(12).TextColor(t.TextMuted).MaxLines(4)
	ui.Divider(c)
	ui.Text(c, "License").Bold().FontSize(13)
	ui.Text(c, "MIT License").TextColor(t.TextMuted)
	ui.Text(c, "Copyright (c) 2026 DumpingGround contributors").TextColor(t.TextMuted)
	ui.Text(c, mitNotice).FontSize(11).TextColor(t.TextMuted).MaxLines(0)
	ui.Divider(c)
	ui.Text(c, "State & logs").Bold().FontSize(13)
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
