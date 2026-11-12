package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/dumpingground/dumpingground/internal/appdir"
	"github.com/dumpingground/dumpingground/internal/applog"
)

// settings is the persisted preference document. The canonical copy lives
// in the per-user folder so every executable location sees it; each known
// portable .data folder gets the same JSON so moving that folder still
// carries preferences to another machine. Unknown keys are retained in
// settingsExtra rather than discarded by the typed struct.
type settings struct {
	ThemeMode       int    `json:"themeMode"`       // 0 system, 1 light, 2 dark
	TabStyle        string `json:"tabStyle"`        // "Pill" | "Underline"
	ComputerLayout  string `json:"computerLayout"`  // "Sidebar" | "Cards"
	Language        string `json:"language"`        // "System" or a display-language name
	ProcessInterval string `json:"processInterval"` // "1 s" | "2 s" | "5 s"
	DiskMetric      string `json:"diskMetric"`      // "Allocated" | "Logical"
	MemRelease      string `json:"memRelease"`      // "Automatic" | "Keep for rescan"
	AnimateMap      bool   `json:"animateMap"`
	TrayMetric      string `json:"trayMetric"` // "CPU" | "Memory" | "GPU" | "Icon"
	CloseToTray     bool   `json:"closeToTray"`
	CheckUpdates    bool   `json:"checkUpdates"`
	// Protected is the user's path whitelist — cleanup never lists or
	// touches these folders or anything under them.
	Protected []string `json:"protected"`
	// PowerPrice is the electricity unit price the user types on the
	// battery page — kept as text so any currency/format survives a
	// round trip; only its parsed value feeds cost estimates.
	PowerPrice string `json:"powerPrice"`
}

// defaults returns the settings a fresh install starts with: the
// sidebar catalogue on the Computer page, a live CPU curve in the tray,
// closing the window quits the app and network update checks stay opt-in.
func defaults() settings {
	return settings{
		TabStyle: "Pill", ComputerLayout: "Sidebar", Language: "System",
		ProcessInterval: "1 s", DiskMetric: "Allocated", MemRelease: "Automatic",
		AnimateMap: true, TrayMetric: "CPU",
	}
}

var knownSettingKeys = map[string]bool{
	"themeMode": true, "tabStyle": true, "computerLayout": true,
	"language": true, "processInterval": true, "diskMetric": true,
	"memRelease": true, "animateMap": true, "trayMetric": true,
	"closeToTray": true, "checkUpdates": true, "protected": true,
	"powerPrice": true,
}

func sharedSettingsPath() string { return filepath.Join(appdir.SharedDir(), "settings.json") }

// settingsPaths returns the shared file first, then every known portable
// copy. The newest occurrence of each key wins during a merge; keys absent
// from the newest file can still be recovered from older copies.
func settingsPaths() []string {
	paths := []string{sharedSettingsPath()}
	for _, dir := range appdir.DataDirs() {
		paths = append(paths, filepath.Join(dir, "settings.json"))
	}
	return paths
}

type settingsDocument struct {
	path    string
	modTime time.Time
	values  map[string]json.RawMessage
}

func readSettingsDocuments() []settingsDocument {
	var docs []settingsDocument
	seen := map[string]bool{}
	for _, path := range settingsPaths() {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var values map[string]json.RawMessage
		if json.Unmarshal(data, &values) != nil {
			applog.Logf("load settings %s: invalid JSON", path)
			continue
		}
		var mod time.Time
		if st, err := os.Stat(path); err == nil {
			mod = st.ModTime()
		}
		docs = append(docs, settingsDocument{path: path, modTime: mod, values: values})
	}
	return docs
}

// mergeSettingsDocuments fills each key from the newest file that has it.
// This imports an older portable copy without letting an incomplete newer
// document erase preferences, and it unions keys this build does not know.
func mergeSettingsDocuments(docs []settingsDocument) map[string]json.RawMessage {
	slices.SortStableFunc(docs, func(a, b settingsDocument) int {
		return b.modTime.Compare(a.modTime)
	})
	merged := map[string]json.RawMessage{}
	for _, doc := range docs {
		for key, value := range doc.values {
			if _, ok := merged[key]; !ok {
				merged[key] = value
			}
		}
	}
	return merged
}

// loadSettings applies the merged preferences over the defaults New()
// already set. A missing or unreadable file leaves the defaults alone.
func (m *Model) loadSettings() {
	appdir.RememberDir(appdir.Dir())
	s := defaults()
	merged := mergeSettingsDocuments(readSettingsDocuments())
	if len(merged) > 0 {
		if data, err := json.Marshal(merged); err == nil {
			_ = json.Unmarshal(data, &s)
		}
	}
	m.settingsExtra = map[string]json.RawMessage{}
	for key, value := range merged {
		if !knownSettingKeys[key] {
			m.settingsExtra[key] = value
		}
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
	m.checkUpdates = s.CheckUpdates
	m.updateAuto.Store(m.checkUpdates)
	m.protected = append(m.protected[:0], s.Protected...)
	m.powerPrice = s.PowerPrice
	m.applySampleInterval()
	m.applyLanguage()
	m.saveSettings() // make the shared and portable copies converge now
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

// saveSettings writes the current preferences to the stable shared file
// and mirrors it into every known portable state folder. A write failure
// only lands in the log — settings are nice to keep, never worth an error
// dialog.
func (m *Model) saveSettings() {
	s := settings{
		ThemeMode: m.themeMode, TabStyle: m.tabStyle, ComputerLayout: m.computerLayout,
		Language: m.language, ProcessInterval: m.processInterval, DiskMetric: m.diskMetric,
		MemRelease: m.memRelease, AnimateMap: m.animateMap,
		TrayMetric: m.trayMetric, CloseToTray: m.closeToTray, CheckUpdates: m.checkUpdates,
		Protected:  m.protected,
		PowerPrice: m.powerPrice,
	}
	known, err := json.Marshal(s)
	if err != nil {
		return
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(known, &values) != nil {
		return
	}
	for key, value := range m.settingsExtra {
		if !knownSettingKeys[key] {
			values[key] = value
		}
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return
	}
	appdir.RememberDir(appdir.Dir())
	paths := []string{sharedSettingsPath()}
	for _, dir := range appdir.DataDirs() {
		paths = append(paths, filepath.Join(dir, "settings.json"))
	}
	seen := map[string]bool{}
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			applog.Logf("save settings: %v", err)
			continue
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			applog.Logf("save settings %s: %v", path, err)
		}
	}
}
