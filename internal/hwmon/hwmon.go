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
	Batt                      *BatteryInfo
	Displays                  []DisplayInfo
	DisplayMode               DisplayMode
	Audio                     []AudioInfo
	Devices                   []DeviceInfo
}

// DisplayInfo describes one monitor, decoded from its EDID — the block
// the monitor itself reports, cached by Windows under
// HKLM\SYSTEM\CurrentControlSet\Enum\DISPLAY.
type DisplayInfo struct {
	Manufacturer string // three-letter PNP id, "LEN", "DEL", …
	Model        string // the monitor-name descriptor when it has one
	Serial       string
	WidthCM      int // physical panel size
	HeightCM     int
	NativeWidth  int
	NativeHeight int
	Year         int
}

// DisplayMode is the mode the primary output is currently running.
type DisplayMode struct {
	Width, Height, RefreshHz, BitsPerPel int
}

// AudioInfo is one audio adapter (SetupAPI's MEDIA class).
type AudioInfo struct {
	Name, Vendor string
}

// DeviceInfo is one present Plug-and-Play device: what it is, its
// Windows device class and who made it. Started mirrors the devnode's
// DN_STARTED flag; Problem is its CM_PROB_* code, 0 when it works.
type DeviceInfo struct {
	Name, Class, Vendor string
	Started             bool
	Problem             uint32
}

// BatteryInfo is the battery's static identity and wear, read from the
// battery driver itself (BATTERY_INFORMATION) rather than estimated from
// the charge percentage.
type BatteryInfo struct {
	DesignCapacity uint64 // mWh as designed
	FullCapacity   uint64 // mWh it holds when full now (health = full/design)
	CycleCount     uint64
	Chemistry      string // "LiP", "LION", …
}

type CPUInfo struct {
	Name, Vendor            string
	Sockets, Cores, Threads int
	BaseMHz                 int
	// PerfCores and EffCores split a hybrid processor by efficiency class
	// (P-cores vs E-cores). Both are 0 on a uniform CPU, where Cores is the
	// whole story.
	PerfCores, EffCores int
	L1, L2, L3          uint64 // bytes, summed over every cache instance
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
	Index          int
	Model          string
	Serial         string
	Firmware       string
	PartitionStyle string // "MBR", "GPT", "RAW" or ""
	Bus            string // "NVMe", "SATA", "USB", …
	Kind           string // "SSD", "HDD" or ""
	Size           uint64
	Letters        []string // "C:", "D:"
	PCIeGen        int      // negotiated link generation (1 = Gen1); 0 unknown
	PCIeLanes      int      // negotiated link width
	PCIeGenMax     int      // what the slot can do
	PCIeLanesMax   int
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
	// Health is the NVMe SMART/Health log, refreshed every 30 s while the
	// Computer page is on screen. nil when the drive does not pass its
	// health log through (SATA, a USB bridge, NVMe behind a RAID driver).
	Health *DiskHealth
	// SATA is the ATA SMART attribute table for a SATA/ATA drive; NVMe
	// drives carry Health instead and leave this nil.
	SATA *SATAHealth
}

type NICSample struct {
	Index                   uint32 // the adapter this reading belongs to (NICInfo.Index)
	Up                      bool
	InRate, OutRate         uint64 // bytes/s
	InTotal, OutTotal       uint64 // bytes since the adapter came up
	LinkBps                 uint64 // negotiated receive speed
	InErrors, OutErrors     uint64 // receive/transmit errors since the adapter came up
	InDiscards, OutDiscards uint64 // packets dropped since the adapter came up
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

// Sample reads the live counters. heavy gates the costlier queries — the
// PDH GPU query, and refreshing the disk health log — which the app only
// asks for while the Computer page is on screen. Not safe for concurrent
// use.
func (m *Monitor) Sample(heavy bool) Sample {
	s := Sample{At: time.Now()}
	m.p.sample(&s, m.info, heavy)
	return s
}
