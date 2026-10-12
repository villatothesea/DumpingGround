package app

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo/ui"
)

// fakeMachine is a laptop-shaped inventory with a minute of synthetic
// history, enough to drive every card and detail page headlessly.
func fakeMachine(m *Model) {
	info := &hwmon.Info{
		Host: "DESKTOP-TEST", User: "dev", OS: "Windows 11 Pro", OSVersion: "24H2", OSBuild: "26100.4061", Arch: "64-bit (x64)",
		Manufacturer: "Acme", Model: "Laptop 9000",
		BIOSVendor: "Acme", BIOSVersion: "1.07", BIOSDate: "04/28/2026", BoardVendor: "Acme", BoardProduct: "Laptop 9000",
		CPU: hwmon.CPUInfo{Name: "Intel(R) Core(TM) Ultra X9 388H", Vendor: "GenuineIntel", Sockets: 1, Cores: 16, Threads: 16, BaseMHz: 3994, L1: 1 << 20, L2: 24 << 20, L3: 18 << 20},
		Mem: hwmon.MemInfo{Total: 64 << 30, Slots: 2, MaxCapacity: 128 << 30, Modules: []hwmon.DIMM{
			{Slot: "DIMM A", Size: 32 << 30, Type: "DDR5", SpeedMTs: 5600, ConfigMTs: 5200, Manufacturer: "Samsung", PartNo: "M425R4GA3BB0"},
			{Slot: "DIMM B", Size: 32 << 30, Type: "DDR5", SpeedMTs: 5600, ConfigMTs: 5200, Manufacturer: "Samsung", PartNo: "M425R4GA3BB0"},
		}},
		GPUs:    []hwmon.GPUInfo{{Name: "NVIDIA GeForce RTX 4070", Vendor: "NVIDIA", DedicatedBytes: 12 << 30, SharedBytes: 32 << 30, Driver: "32.0.15.6094", DriverDate: "2024-10-01"}},
		Disks:   []hwmon.DiskInfo{{Index: 0, Model: "Samsung 990 PRO 2TB", Bus: "NVMe", Kind: "SSD", Size: 2000 << 30, Letters: []string{"C:"}}, {Index: 1, Model: "Crucial MX500 4TB", Bus: "SATA", Kind: "SSD", Size: 4000 << 30, Letters: []string{"D:"}}},
		NICs:    []hwmon.NICInfo{{Index: 10, Name: "WLAN", Description: "Intel Wi-Fi 7 BE211", Kind: "Wi-Fi", MAC: "E0-C9-32-AD-FF-B1", MTU: 1500, IPs: []string{"192.168.1.20", "2001:db8::1"}}},
		Battery: true,
	}
	m.hw.info.Store(info)
	now := time.Now()
	for i := 0; i < 90; i++ {
		f := float64(i) / 10
		s := hwmon.Sample{
			At:  now.Add(time.Duration(i-90) * time.Second),
			CPU: 35 + 30*math.Sin(f), MHz: 3200, Procs: 312, Threads: 4521, Handles: 120345, Uptime: 26*time.Hour + 6*time.Minute,
			MemTotal: 64 << 30, MemUsed: uint64(24+4*math.Sin(f/3)) << 30, MemAvail: 36 << 30, MemCached: 20 << 30, Commit: 40 << 30, CommitLimit: 80 << 30, PagedPool: 400 << 20, NonPagedPool: 300 << 20,
			GPUs: []hwmon.GPUSample{{Util: 20 + 15*math.Sin(f*1.3), HasUtilization: true, HasMemory: true, DedicatedUsed: 3 << 30, SharedUsed: 1 << 30,
				Engines: map[string]float64{"3D": 20 + 15*math.Sin(f*1.3), "VideoDecode": 8 + 6*math.Sin(f), "Copy": 2}}},
			Disks: []hwmon.DiskSample{
				{Active: 10 + 8*math.Sin(f), ReadRate: uint64(30e6 * (1 + math.Sin(f))), WriteRate: 5e6, RespMs: 0.4, Queue: 1,
					Health: &hwmon.DiskHealth{OK: true, TempC: 41, AvailableSpare: 100, AvailableSpareThreshold: 5, PercentageUsed: 3, DataRead: 6 << 40, DataWritten: 12 << 40,
						PowerOnHours: 5000, PowerCycles: 120, UnsafeShutdowns: 4, ErrorLogEntries: 2}},
				{Active: 3},
			},
			NICs:    []hwmon.NICSample{{Index: 10, Up: true, InRate: uint64(2e6 * (1.2 + math.Sin(f/2))), OutRate: 200e3, InTotal: 3 << 30, OutTotal: 1 << 30, LinkBps: 865e6}},
			Battery: &hwmon.BatterySample{Percent: 87, OnAC: true, Charging: true},
		}
		for c := 0; c < 16; c++ {
			s.Cores = append(s.Cores, math.Abs(60*math.Sin(f+float64(c))))
		}
		m.hw.hist = append(m.hw.hist, s)
	}
}

// TestDiskHealthPanel checks the NVMe verdict panel: a readable drive shows
// its figures, and a drive whose health log cannot be read says so instead
// of claiming to be healthy.
func TestDiskHealthPanel(t *testing.T) {
	m := New()
	m.page = computerPage
	fakeMachine(m)
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()
	if err := tt.Click("Disk 0 (C:)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Health", "Good", "Endurance used", "Data written", "Media errors"} {
		if !tt.HasText(want) {
			t.Errorf("disk health panel is missing %q", want)
		}
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Disk 1 (D:)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Health information is not available for this drive.") {
		t.Error("a drive without a health log did not say so")
	}
	if tt.HasText("judged from the drive's own counters") {
		t.Error("a drive without a health log showed a verdict")
	}
}

// TestComputerPageOverviewAndDetail
func TestComputerPageOverviewAndDetail(t *testing.T) {
	m := New()
	m.page = computerPage
	fakeMachine(m)
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()
	for _, want := range []string{"Computer", "DESKTOP-TEST", "CPU", "Memory", "GPU", "Disk 0 (C:)", "Disk 1 (D:)", "WLAN", "Battery"} {
		if !tt.HasText(want) {
			t.Errorf("overview is missing %q", want)
		}
	}
	for _, card := range []struct{ click, want string }{
		{"CPU", "Logical processors"},
		{"Memory", "Memory modules"},
		{"GPU", "Engines"},
		{"Disk 0 (C:)", "Transfer rate"},
		{"WLAN", "Throughput"},
		{"Battery", "Charge level"},
	} {
		if err := tt.Click(card.click); err != nil {
			t.Fatalf("click %q: %v", card.click, err)
		}
		tt.Frame()
		if m.hw.detail == "" || !tt.HasText(card.want) || !tt.HasText("Back") {
			t.Fatalf("clicking %q: detail=%q, wanted %q on screen", card.click, m.hw.detail, card.want)
		}
		if err := tt.Click("Back"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if m.hw.detail != "" {
			t.Fatalf("Back did not return to the overview from %q", card.click)
		}
	}
	// Esc leaves a detail page too.
	tt.Click("CPU")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.hw.detail != "" {
		t.Fatal("Esc did not return to the overview")
	}
	// Re-clicking the active Computer tab also goes home.
	tt.Click("CPU")
	tt.Frame()
	tt.Click("Computer")
	tt.Frame()
	if m.hw.detail != "" {
		t.Fatal("clicking the Computer tab did not return to the overview")
	}
}

// TestComputerLiveSampler runs the real sampling goroutine against this
// machine and renders its data — the end-to-end check that inventory,
// history and every card build from live readings.
func TestComputerLiveSampler(t *testing.T) {
	if testing.Short() {
		t.Skip("takes a few seconds")
	}
	m := New()
	m.page = computerPage
	defer m.close()
	m.startHardware()
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	deadline := time.Now().Add(15 * time.Second)
	for {
		tt.Frame() // marks the page visible so the sampler includes the GPU
		m.hw.mu.Lock()
		n := len(m.hw.hist)
		m.hw.mu.Unlock()
		if n >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d samples after 15 s", n)
		}
		time.Sleep(300 * time.Millisecond)
	}
	tt.Frame()
	info := m.hw.info.Load()
	if info == nil || info.Host == "" || !tt.HasText(info.Host) || !tt.HasText("CPU") || !tt.HasText("Memory") {
		t.Fatalf("live page incomplete: info=%+v", info)
	}
	for _, id := range []string{"cpu", "mem"} {
		m.hw.detail = id
		tt.Frame()
	}
	if dest := os.Getenv("DG_SHOTS"); dest != "" {
		m.hw.detail = ""
		tt.Frame()
		dump(t, tt, dest, "computer-live-overview.png")
		m.hw.detail = "cpu"
		tt.Frame()
		dump(t, tt, dest, "computer-live-cpu.png")
	}
}

func TestComputerPageBeforeInventoryLoads(t *testing.T) {
	m := New()
	m.page = computerPage
	tt := ui.NewTester(m.view, 1180, 760)
	tt.Frame()
	if !tt.HasText("Reading hardware…") {
		t.Fatal("no loading state")
	}
}

func TestComputerPageNarrowAndEmpty(t *testing.T) {
	m := New()
	m.page = computerPage
	fakeMachine(m)
	m.hw.hist = nil // no samples yet: every card must still render
	ui.NewTester(m.view, 960, 580).Frame()
	m.hw.detail = "gpu:0"
	ui.NewTester(m.view, 960, 580).Frame()
	m.hw.detail = "disk:7" // stale id falls back to the overview
	tt := ui.NewTester(m.view, 960, 580)
	tt.Frame()
	if m.hw.detail != "" {
		t.Fatal("stale detail id was kept")
	}
}

func TestChartScale(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{{0, 1}, {1, 1}, {1.1, 2}, {3, 5}, {5.1, 10}, {99, 100}, {120e6, 200e6}} {
		if got := niceCeil(tc.in); got != tc.want {
			t.Errorf("niceCeil(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestComputerScreenshots writes PNGs of the page when DG_SHOTS is set.
func TestComputerScreenshots(t *testing.T) {
	dest := os.Getenv("DG_SHOTS")
	if dest == "" {
		t.Skip("DG_SHOTS not set")
	}
	os.MkdirAll(dest, 0o755)
	// The window's minimum size: three columns of cards must still fit.
	nm := New()
	nm.page = computerPage
	fakeMachine(nm)
	ntt := ui.NewTester(nm.view, 960, 580)
	ntt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	ntt.Frame()
	dump(t, ntt, dest, "computer-overview-min.png")
	nm.hw.detail = "cpu"
	ntt.Frame()
	dump(t, ntt, dest, "computer-cpu-detail-min.png")
	for _, dark := range []bool{false, true} {
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		m := New()
		m.page = computerPage
		fakeMachine(m)
		tt := ui.NewTester(m.view, 1180, 760)
		tt.SetDark(dark)
		tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
		tt.Frame()
		dump(t, tt, dest, "computer-overview-"+suffix+".png")
		for _, id := range []string{"cpu", "mem", "gpu:0", "disk:0", "net:10"} {
			m.hw.detail = id
			tt.Frame()
			dump(t, tt, dest, "computer-"+filepath.Base(id[:3])+"-detail-"+suffix+".png")
		}
	}
}
