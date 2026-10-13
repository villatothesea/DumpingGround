package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/dumpingground/dumpingground/internal/appdir"
	"github.com/dumpingground/dumpingground/internal/applog"
)

// settings is the persisted preference file, written to settings.json
// beside the rest of the portable state. Every field has a default that
// is used when the file is absent or a key is missing.
type settings struct {
	ThemeMode       int    `json:"themeMode"`       // 0 system, 1 light, 2 dark
	TabStyle        string `json:"tabStyle"`        // "Pill" | "Underline"
	ComputerLayout  string `json:"computerLayout"`  // "Sidebar" | "Cards"
	Language        string `json:"language"`        // reserved: "System" | "English" | "中文"
	ProcessInterval string `json:"processInterval"` // "1 s" | "2 s" | "5 s"
	DiskMetric      string `json:"diskMetric"`      // "Allocated" | "Logical"
	MemRelease      string `json:"memRelease"`      // "Automatic" | "Keep for rescan"
	AnimateMap      bool   `json:"animateMap"`
	TrayMetric      string `json:"trayMetric"` // "CPU" | "Memory" | "GPU" | "Icon"
	CloseToTray     bool   `json:"closeToTray"`
}

// defaults returns the settings a fresh install starts with: the
// sidebar catalogue on the Computer page, a live CPU curve in the tray
// and closing the window quits the app.
func defaults() settings {
	return settings{
		TabStyle: "Pill", ComputerLayout: "Sidebar", Language: "System",
		ProcessInterval: "1 s", DiskMetric: "Allocated", MemRelease: "Automatic",
		AnimateMap: true, TrayMetric: "CPU",
	}
}

func settingsPath() string { return filepath.Join(appdir.Dir(), "settings.json") }

// loadSettings applies the saved preferences over the defaults New()
// already set. A missing or unreadable file leaves the defaults alone.
func (m *Model) loadSettings() {
	s := defaults()
	if data, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	m.themeMode = s.ThemeMode
	m.tabStyle = s.TabStyle
	m.computerLayout = s.ComputerLayout
	m.language = s.Language
	if m.language == "中文" { // the pre-localization placeholder
		m.language = "简体中文"
	}
	m.processInterval = s.ProcessInterval
	m.diskMetric = s.DiskMetric
	m.memRelease = s.MemRelease
	m.animateMap = s.AnimateMap
	m.trayMetric = s.TrayMetric
	m.closeToTray = s.CloseToTray
	m.applySampleInterval()
	m.applyLanguage()
}

// applySampleInterval maps the saved interval label onto the sampler.
func (m *Model) applySampleInterval() {
	switch m.processInterval {
	case "2 s":
		m.sampleInterval.Store(2e9)
	case "5 s":
		m.sampleInterval.Store(5e9)
	default:
		m.sampleInterval.Store(1e9)
	}
}

// saveSettings writes the current preferences; a write failure only
// lands in the log — settings are nice to keep, never worth an error
// dialog.
func (m *Model) saveSettings() {
	s := settings{
		ThemeMode: m.themeMode, TabStyle: m.tabStyle, ComputerLayout: m.computerLayout,
		Language: m.language, ProcessInterval: m.processInterval, DiskMetric: m.diskMetric,
		MemRelease: m.memRelease, AnimateMap: m.animateMap,
		TrayMetric: m.trayMetric, CloseToTray: m.closeToTray,
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(settingsPath(), data, 0o644); err != nil {
		applog.Logf("save settings: %v", err)
	}
}
