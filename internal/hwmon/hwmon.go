// Package hwmon reads the machine's hardware inventory and live utilization
// without drivers or third-party libraries: SMBIOS, the registry, DXGI, PDH
// counters, disk-performance IOCTLs and the IP helper tables. Info is
// gathered once (slow — call it off the UI thread); Sample is cheap enough to
// run at 1 Hz.
package hwmon

import (
	"slices"
	"time"
)

// Info is the static description of the machine.
type Info struct {
	Host, User                string
	OS, OSVersion, OSBuild    string // "Windows 11 Pro", "24H2", "26100.4061"
	Arch                      string
	Manufacturer, Model       string
	BIOSVendor, BIOSVersion   string
	BIOSDate                  string
	BoardVendor, BoardProduct string
	CPU                       CPUInfo
	Mem                       MemInfo
	GPUs                      []GPUInfo
	Disks                     []DiskInfo
	NICs                      []NICInfo
	Battery                   bool
}

type CPUInfo struct {
	Name, Vendor            string
	Sockets, Cores, Threads int
	BaseMHz                 int
	L1, L2, L3              uint64 // bytes, summed over every cache instance
}

type MemInfo struct {
	Total       uint64
	Modules     []DIMM
	Slots       int    // physical slots the board reports, 0 if unknown
	MaxCapacity uint64 // bytes the board supports, 0 if unknown
}

type DIMM struct {
	Slot, Bank           string
	Size                 uint64
	Type, FormFactor     string
	SpeedMTs, ConfigMTs  int
	Manufacturer, PartNo string
}

type GPUInfo struct {
	Name                        string
	Vendor                      string
	DedicatedBytes, SharedBytes uint64
	Driver, DriverDate          string
	// luids are the PDH instance keys, "0x%08x_0x%08x". One physical
	// adapter can enumerate under several LUIDs (Windows lists some iGPUs
	// twice), so counters are merged across them.
	luids []string
}

type DiskInfo struct {
	Index   int
	Model   string
	Bus     string // "NVMe", "SATA", "USB", …
	Kind    string // "SSD", "HDD" or ""
	Size    uint64
	Letters []string // "C:", "D:"
}

type NICInfo struct {
	Index       uint32
	Name        string // the adapter's alias, "Ethernet", "Wi-Fi"
	Description string
	Kind        string
	MAC         string
	MTU         uint32
	IPs         []string
}

// Sample is one reading of the live counters; slices run parallel to the
// matching Info slices.
type Sample struct {
	At time.Time

	CPU     float64   // 0–100
	Cores   []float64 // per logical processor, 0–100
	MHz     int       // current speed, averaged over processors
	Procs   uint32
	Threads uint32
	Handles uint32
	Uptime  time.Duration

	MemTotal, MemUsed, MemAvail, MemCached uint64
	Commit, CommitLimit                    uint64
	PagedPool, NonPagedPool                uint64

	// GPUs is nil on ticks that skipped the (costlier) GPU query.
	GPUs  []GPUSample
	Disks []DiskSample
	NICs  []NICSample

	Battery *BatterySample
}

type GPUSample struct {
	Util                      float64            // busiest engine, 0–100
	Engines                   map[string]float64 // by engine type, 0–100
	DedicatedUsed, SharedUsed uint64
	HasMemory, HasUtilization bool
}

type DiskSample struct {
	Active                float64 // % of time the disk was busy, 0–100
	ReadRate, WriteRate   uint64  // bytes/s
	RespMs                float64 // average response time over the interval
	Queue                 uint32
	ReadBytes, WriteBytes uint64 // since boot
}

type NICSample struct {
	Index             uint32 // the adapter this reading belongs to (NICInfo.Index)
	Up                bool
	InRate, OutRate   uint64 // bytes/s
	InTotal, OutTotal uint64 // bytes since the adapter came up
	LinkBps           uint64 // negotiated receive speed
}

type BatterySample struct {
	Percent  int // 0–100, -1 unknown
	Charging bool
	OnAC     bool
	Remain   time.Duration // 0 when unknown
}

// Monitor owns the platform state Sample needs between ticks (previous
// counters for rates, open handles, the PDH query).
type Monitor struct {
	info *Info
	p    platform
}

func New() *Monitor { return &Monitor{info: &Info{}} }

// Load gathers the static inventory. It takes hundreds of milliseconds
// (SMBIOS, DXGI, opening every physical drive) — call it off the UI
// thread. Call it once, before Sample.
func (m *Monitor) Load() *Info {
	m.p.load(m.info)
	return m.info
}

// RefreshNICs re-reads the connected adapters — an Ethernet cable, a VPN or
// a hotspot comes and goes while the app runs. When the set changed it
// returns a fresh Info (the one already handed out is being read by the UI
// and is never mutated), otherwise nil. Call it from the sampling goroutine.
func (m *Monitor) RefreshNICs() *Info {
	nics := m.p.enumNICs()
	if slices.EqualFunc(nics, m.info.NICs, func(a, b NICInfo) bool {
		return a.Index == b.Index && a.Name == b.Name && a.Description == b.Description && slices.Equal(a.IPs, b.IPs)
	}) {
		return nil
	}
	n := *m.info
	n.NICs = nics
	m.info = &n
	return m.info
}

// Sample reads the live counters. withGPU gates the PDH query, the only
// part that is not trivially cheap. Not safe for concurrent use.
func (m *Monitor) Sample(withGPU bool) Sample {
	s := Sample{At: time.Now()}
	m.p.sample(&s, m.info, withGPU)
	return s
}
