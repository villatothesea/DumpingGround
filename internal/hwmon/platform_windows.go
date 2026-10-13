//go:build windows

package hwmon

import (
	"encoding/binary"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

var (
	k32      = syscall.NewLazyDLL("kernel32.dll")
	ntdll    = syscall.NewLazyDLL("ntdll.dll")
	powrprof = syscall.NewLazyDLL("powrprof.dll")
	iphlpapi = syscall.NewLazyDLL("iphlpapi.dll")
	pdhDLL   = syscall.NewLazyDLL("pdh.dll")
	dxgiDLL  = syscall.NewLazyDLL("dxgi.dll")
	setupapi = syscall.NewLazyDLL("setupapi.dll")
	cfgmgr32 = syscall.NewLazyDLL("cfgmgr32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	procGetSystemFirmwareTable   = k32.NewProc("GetSystemFirmwareTable")
	procGetLogicalProcInfoEx     = k32.NewProc("GetLogicalProcessorInformationEx")
	procGetActiveProcessorCount  = k32.NewProc("GetActiveProcessorCount")
	procGetPerformanceInfo       = k32.NewProc("K32GetPerformanceInfo")
	procGetTickCount64           = k32.NewProc("GetTickCount64")
	procGetSystemPowerStatus     = k32.NewProc("GetSystemPowerStatus")
	procGetDriveType             = k32.NewProc("GetDriveTypeW")
	procNtQuerySystemInformation = ntdll.NewProc("NtQuerySystemInformation")
	procCallNtPowerInformation   = powrprof.NewProc("CallNtPowerInformation")
	procGetIfTable2              = iphlpapi.NewProc("GetIfTable2")
	procGetIfEntry2              = iphlpapi.NewProc("GetIfEntry2")
	procFreeMibTable             = iphlpapi.NewProc("FreeMibTable")
	procGlobalMemoryStatusEx     = k32.NewProc("GlobalMemoryStatusEx")

	procSetupDiGetClassDevs          = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces  = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiEnumDeviceInfo        = setupapi.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiGetDeviceInterfaceDet = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiGetDeviceRegistryProp = setupapi.NewProc("SetupDiGetDeviceRegistryPropertyW")
	procSetupDiGetDeviceInstanceID   = setupapi.NewProc("SetupDiGetDeviceInstanceIdW")
	procSetupDiDestroyDeviceInfoList = setupapi.NewProc("SetupDiDestroyDeviceInfoList")

	procCMGetDevNodeProp = cfgmgr32.NewProc("CM_Get_DevNode_PropertyW")
	procCMGetParent      = cfgmgr32.NewProc("CM_Get_Parent")

	procEnumDisplaySettings = user32.NewProc("EnumDisplaySettingsW")
)

// platform holds what Sample needs to turn cumulative counters into rates.
type platform struct {
	cores    []coreTimes
	disks    []*diskHandle
	nics     map[uint32]nicPrev
	gpu      gpuState
	perf     perfInfo // GetPerformanceInfo walks the process list: refreshed every perfEvery
	perfAt   time.Time
	healthAt time.Time // last NVMe health refresh
}

func (p *platform) load(info *Info) {
	loadOS(info)
	loadSMBIOS(info)
	loadCPU(info)
	loadMemory(info)
	p.loadGPUs(info)
	p.loadDisks(info)
	p.loadNICs(info)
	info.Battery = batteryPresent()
	if info.Battery {
		info.Batt = readBatteryInfo()
	}
	loadDisplays(info, loadDevices(info))
}

func le64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

// utf16Bytes decodes little-endian UTF-16 up to the first NUL.
func utf16Bytes(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

func regOpen(path string) (syscall.Handle, bool) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	var h syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, p, 0, syscall.KEY_READ, &h) != nil {
		return 0, false
	}
	return h, true
}

// regOpenSub opens a child key of an already-open key.
func regOpenSub(parent syscall.Handle, name string) (syscall.Handle, bool) {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, false
	}
	var h syscall.Handle
	if syscall.RegOpenKeyEx(parent, p, 0, syscall.KEY_READ, &h) != nil {
		return 0, false
	}
	return h, true
}

// regSubkeys lists a key's immediate child key names.
func regSubkeys(h syscall.Handle) []string {
	var names []string
	for i := uint32(0); ; i++ {
		buf := make([]uint16, 256)
		n := uint32(len(buf))
		if syscall.RegEnumKeyEx(h, i, &buf[0], &n, nil, nil, nil, nil) != nil {
			break
		}
		names = append(names, syscall.UTF16ToString(buf[:n]))
	}
	return names
}

func regRead(h syscall.Handle, name string) []byte {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil
	}
	var typ, size uint32
	if syscall.RegQueryValueEx(h, n, nil, &typ, nil, &size) != nil || size == 0 {
		return nil
	}
	buf := make([]byte, size)
	if syscall.RegQueryValueEx(h, n, nil, &typ, &buf[0], &size) != nil {
		return nil
	}
	return buf[:size]
}

func regStr(h syscall.Handle, name string) string {
	return strings.Join(strings.Fields(utf16Bytes(regRead(h, name))), " ")
}

func regDword(h syscall.Handle, name string) (uint32, bool) {
	b := regRead(h, name)
	if len(b) < 4 {
		return 0, false
	}
	return le32(b), true
}
