package app

import (
	"bytes"
	"image/png"
	"math"
	"testing"

	"github.com/dumpingground/dumpingground/internal/appdir"
	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo/ui"
)

// TestSettingsRoundTrip saves a set of preferences, loads them into a
// fresh model and checks every value made the trip; a missing file must
// leave the defaults alone.
func TestSettingsRoundTrip(t *testing.T) {
	appdir.SetDir(t.TempDir())
	defer appdir.SetDir("")

	m := New()
	m.themeMode = 2
	m.computerLayout = "Cards"
	m.trayMetric = "Memory"
	m.closeToTray = true
	m.processInterval = "5 s"
	m.saveSettings()

	fresh := New()
	fresh.loadSettings()
	if fresh.themeMode != 2 || fresh.computerLayout != "Cards" || fresh.trayMetric != "Memory" ||
		!fresh.closeToTray || fresh.processInterval != "5 s" {
		t.Errorf("settings did not round-trip: %+v", fresh)
	}
	if fresh.sampleInterval.Load() != 5e9 {
		t.Errorf("sampleInterval = %d, want 5e9", fresh.sampleInterval.Load())
	}
}

// TestSettingsDefaults keeps every default when no file exists.
func TestSettingsDefaults(t *testing.T) {
	appdir.SetDir(t.TempDir())
	defer appdir.SetDir("")

	m := New()
	m.loadSettings()
	if m.computerLayout != "Sidebar" || m.trayMetric != "CPU" || m.closeToTray ||
		m.language != "System" || m.themeMode != 0 {
		t.Errorf("defaults drifted: layout=%q tray=%q close=%v lang=%q theme=%d",
			m.computerLayout, m.trayMetric, m.closeToTray, m.language, m.themeMode)
	}
}

// TestSettingsWindow exercises the catalogue rail: sections switch, the
// About page carries the MIT notice and the tray section offers GPU only
// when a discrete adapter exists.
func TestSettingsWindow(t *testing.T) {
	m := New()
	m.page = computerPage
	fakeMachine(m)
	m.settingsOpen = true
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()

	for _, want := range []string{"Display & language", "Processes & scanning", "Tray & window", "About", "Theme", "Computer page", "Language"} {
		if !tt.HasText(want) {
			t.Errorf("settings missing %q", want)
		}
	}
	if err := tt.Click("Tray & window"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Tray icon shows", "Close button"} {
		if !tt.HasText(want) {
			t.Errorf("tray section missing %q", want)
		}
	}
	if err := tt.Click("About"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"DumpingGround", "Version " + version, "MIT License", "DumpingGround contributors", "Permission is hereby granted"} {
		if !tt.HasText(want) {
			t.Errorf("about page missing %q", want)
		}
	}
}

// TestDiscreteGPU gates the tray's GPU option on real hardware.
func TestDiscreteGPU(t *testing.T) {
	var info hwmon.Info
	if hasDiscreteGPU(&info) {
		t.Error("empty inventory should not offer GPU")
	}
	info.GPUs = []hwmon.GPUInfo{{Name: "UHD iGPU"}}
	if hasDiscreteGPU(&info) {
		t.Error("integrated adapter should not offer GPU")
	}
	info.GPUs = append(info.GPUs, hwmon.GPUInfo{Name: "RTX", DedicatedBytes: 8 << 30})
	if !hasDiscreteGPU(&info) {
		t.Error("dedicated-memory adapter should offer GPU")
	}
}

// TestTrayIconPNG feeds a synthetic history through the renderer and
// checks it produces a decodable 32×32 PNG for every metric.
func TestTrayIconPNG(t *testing.T) {
	hist := make([]hwmon.Sample, 90)
	for i := range hist {
		f := float64(i) / 10
		hist[i] = hwmon.Sample{
			CPU:      50 + 40*math.Sin(f),
			MemTotal: 64 << 30,
			MemUsed:  uint64(32+8*math.Sin(f/2)) << 30,
			GPUs:     []hwmon.GPUSample{{Util: 30 + 20*math.Sin(f*1.7), HasUtilization: true}},
		}
	}
	for _, metric := range []string{"CPU", "Memory", "GPU"} {
		data := trayIconPNG(hist, metric, hwCPUCol)
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", metric, err)
		}
		if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
			t.Errorf("%s: %v, want 32×32", metric, img.Bounds())
		}
	}
	// A GPU history without utilization counters draws zeros, not a
	// bogus flat line at the top.
	hist[0].GPUs = []hwmon.GPUSample{{}}
	if _, ok := traySeries(&hist[0], "GPU"); ok {
		t.Error("GPU series without utilization reported a value")
	}
}

// TestTrayValue picks the configured figure from a sample.
func TestTrayValue(t *testing.T) {
	m := New()
	s := hwmon.Sample{CPU: 42, MemTotal: 64 << 30, MemUsed: 32 << 30,
		GPUs: []hwmon.GPUSample{{Util: 77, HasUtilization: true}}}
	for metric, want := range map[string]float64{"CPU": 42, "Memory": 50, "GPU": 77} {
		m.trayMetric = metric
		got, _, _, ok := m.trayValue(s)
		if !ok || math.Abs(got-want) > 0.5 {
			t.Errorf("%s: got %.1f,%v want %.0f", metric, got, ok, want)
		}
	}
}

// TestComputerCardsLayout renders the restored card overview and makes
// sure cards still open their details.
func TestComputerCardsLayout(t *testing.T) {
	m := New()
	m.page = computerPage
	m.computerLayout = "Cards"
	fakeMachine(m)
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()
	if !tt.HasText("DESKTOP-TEST") {
		t.Error("cards overview lost the machine header")
	}
	if err := tt.Click("CPU"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Utilization") {
		t.Error("CPU card did not open its detail")
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.hw.detail != "" {
		t.Error("Back did not return to the card grid")
	}
}
