//go:build windows

package hwmon

import (
	"testing"
	"time"
)

// TestLive reads this machine and logs everything: it is how the struct
// offsets (SMBIOS, MIB_IF_ROW2, DXGI, PDH) were verified, and it asserts
// only what every Windows machine has.
func TestLive(t *testing.T) {
	m := New()
	start := time.Now()
	info := m.Load()
	t.Logf("Load took %v", time.Since(start))
	t.Logf("Host=%q User=%q OS=%q %q build %q Arch=%q", info.Host, info.User, info.OS, info.OSVersion, info.OSBuild, info.Arch)
	t.Logf("System: %q %q | Board %q %q | BIOS %q %q %q", info.Manufacturer, info.Model, info.BoardVendor, info.BoardProduct, info.BIOSVendor, info.BIOSVersion, info.BIOSDate)
	t.Logf("CPU: %+v", info.CPU)
	t.Logf("Mem: total=%d slots=%d max=%d", info.Mem.Total, info.Mem.Slots, info.Mem.MaxCapacity)
	for _, d := range info.Mem.Modules {
		t.Logf("  DIMM %+v", d)
	}
	for _, g := range info.GPUs {
		t.Logf("GPU %+v", g)
	}
	for _, d := range info.Disks {
		t.Logf("Disk %+v", d)
	}
	for _, n := range info.NICs {
		t.Logf("NIC %+v", n)
	}
	t.Logf("Battery=%v", info.Battery)

	if info.Host == "" || info.OS == "" || info.CPU.Name == "" || info.CPU.Threads == 0 || info.Mem.Total == 0 {
		t.Errorf("basic inventory missing: %+v", info)
	}

	var last Sample
	var slowest time.Duration
	for i := 0; i < 4; i++ {
		time.Sleep(1100 * time.Millisecond)
		t0 := time.Now()
		last = m.Sample(true)
		slowest = max(slowest, time.Since(t0))
	}
	t.Logf("slowest Sample(true): %v", slowest)
	t.Logf("CPU %.1f%% @ %d MHz, %d cores, procs=%d threads=%d handles=%d uptime=%v", last.CPU, last.MHz, len(last.Cores), last.Procs, last.Threads, last.Handles, last.Uptime.Round(time.Second))
	t.Logf("Mem used=%d avail=%d cached=%d commit=%d/%d paged=%d nonpaged=%d", last.MemUsed, last.MemAvail, last.MemCached, last.Commit, last.CommitLimit, last.PagedPool, last.NonPagedPool)
	for i, g := range last.GPUs {
		t.Logf("GPU[%d] util=%.1f engines=%v ded=%d shared=%d hasUtil=%v hasMem=%v", i, g.Util, g.Engines, g.DedicatedUsed, g.SharedUsed, g.HasUtilization, g.HasMemory)
	}
	for i, d := range last.Disks {
		t.Logf("Disk[%d] active=%.1f%% read=%d/s write=%d/s resp=%.2fms q=%d health=%+v", i, d.Active, d.ReadRate, d.WriteRate, d.RespMs, d.Queue, d.Health)
	}
	for i, n := range last.NICs {
		t.Logf("NIC[%d] up=%v in=%d/s out=%d/s link=%d totals=%d/%d", i, n.Up, n.InRate, n.OutRate, n.LinkBps, n.InTotal, n.OutTotal)
	}
	if last.Battery != nil {
		t.Logf("Battery %+v", *last.Battery)
	}
	if len(last.Cores) != info.CPU.Threads {
		t.Errorf("cores %d != threads %d", len(last.Cores), info.CPU.Threads)
	}
	if last.MemTotal == 0 || last.MemUsed == 0 || last.Procs == 0 || last.Uptime == 0 {
		t.Errorf("memory/process counters empty: %+v", last)
	}
}

func TestSMBIOSParse(t *testing.T) {
	// Type 1 (System): manufacturer + product strings, then type 127.
	raw := []byte{0, 3, 0, 0, 0, 0, 0, 0} // RawSMBIOSData header
	table := []byte{
		1, 8, 1, 0, 1, 2, 0, 0, // type 1, length 8, handle, mfr=1, product=2, version=0, serial=0
		'A', 'c', 'm', 'e', 0, 'B', 'o', 'x', 0, 0,
		127, 4, 2, 0, 0, 0,
	}
	raw[4] = byte(len(table))
	sm := parseSMBIOS(append(raw, table...))
	if sm.SysMfr != "Acme" || sm.SysProduct != "Box" {
		t.Fatalf("got %+v", sm)
	}
	// Truncated and junk input must not panic.
	parseSMBIOS(nil)
	parseSMBIOS([]byte{0, 0, 0, 0, 9, 0, 0, 0, 17, 40})
}
