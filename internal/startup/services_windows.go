//go:build windows

package startup

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procOpenSCManager        = advapi32.NewProc("OpenSCManagerW")
	procOpenService          = advapi32.NewProc("OpenServiceW")
	procCloseServiceHandle   = advapi32.NewProc("CloseServiceHandle")
	procEnumServicesStatusEx = advapi32.NewProc("EnumServicesStatusExW")
	procChangeServiceConfig  = advapi32.NewProc("ChangeServiceConfigW")
	procChangeServiceConfig2 = advapi32.NewProc("ChangeServiceConfig2W")
)

const (
	scManagerConnect       = 0x1
	scManagerEnumerate     = 0x4
	serviceQueryConfig     = 0x1
	serviceChangeConfig    = 0x2
	serviceNoChange        = 0xFFFFFFFF
	serviceAutoStart       = 2
	serviceDisabled        = 4
	serviceRunning         = 4
	configDelayedAutoStart = 3
	errMoreData            = 234
	servicesKey            = `SYSTEM\CurrentControlSet\Services\`
)

type svcEntry struct {
	name, display string
	typ, state    uint32
}

// enumServices returns every Win32 service the SCM knows, with its
// current state. The buffer holds the entry array followed by the strings
// it points at, so the pointers are turned back into offsets.
func enumServices() []svcEntry {
	scm, _, _ := procOpenSCManager.Call(0, 0, scManagerConnect|scManagerEnumerate)
	if scm == 0 {
		return nil
	}
	defer procCloseServiceHandle.Call(scm)
	const entry = 56 // ENUM_SERVICE_STATUS_PROCESSW on x64
	var out []svcEntry
	var resume uint32
	buf := make([]byte, 256<<10)
	base := uintptr(unsafe.Pointer(&buf[0]))
	str := func(p uint64) string {
		off := int(uintptr(p) - base)
		if off < 0 || off >= len(buf) {
			return ""
		}
		return utf16z(buf[off:])
	}
	for {
		var need, count uint32
		r, _, e := procEnumServicesStatusEx.Call(scm, 0, 0x30, 3, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
			uintptr(unsafe.Pointer(&need)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&resume)), 0)
		if r == 0 && e != syscall.Errno(errMoreData) {
			return out
		}
		for i := 0; i < int(count) && (i+1)*entry <= len(buf); i++ {
			b := buf[i*entry:]
			out = append(out, svcEntry{name: str(le64(b)), display: str(le64(b[8:])), typ: le32(b[16:]), state: le32(b[20:])})
		}
		if r != 0 { // the last batch
			return out
		}
	}
}

// listServices returns the services that start by themselves — automatic,
// delayed, or switched off — not the ones that only run on demand.
func listServices() []Item {
	var items []Item
	for _, s := range enumServices() {
		if s.typ&0xC0 != 0 || s.name == "" { // per-user service instances and templates
			continue
		}
		h, err := openKey(syscall.HKEY_LOCAL_MACHINE, servicesKey+s.name, syscall.KEY_QUERY_VALUE)
		if err != nil {
			continue
		}
		startB := queryValue(h, "Start")
		image := strings.TrimSpace(utf16z(queryValue(h, "ImagePath")))
		delayedB := queryValue(h, "DelayedAutostart")
		syscall.RegCloseKey(h)
		if len(startB) < 4 || image == "" {
			continue
		}
		start := le32(startB)
		if start != serviceAutoStart && start != serviceDisabled {
			continue
		}
		delayed := len(delayedB) >= 4 && le32(delayedB) != 0
		trigger := "Automatic"
		switch {
		case start == serviceDisabled:
			trigger = "Disabled"
		case delayed:
			trigger = "Automatic (delayed)"
		}
		exe, _ := splitCommand(image)
		name := s.display
		if name == "" {
			name = s.name
		}
		reg := `HKLM\` + servicesKey + s.name
		items = append(items, Item{
			Kind: KindService, Key: "service|" + s.name, Name: name, Command: expandEnv(image), Exe: exe,
			Location: "Service " + s.name, reg: reg, Scope: "System", Trigger: trigger,
			Enabled: start != serviceDisabled, Running: s.state == serviceRunning,
			value: s.name, start: start, delayed: delayed,
		})
	}
	return items
}

// setServiceEnabled switches a service's start type. Disabling remembers
// the original (automatic, and whether delayed) so enabling restores it
// exactly; a running service is left running until the next boot — the
// switch is about what starts next time.
func setServiceEnabled(it Item, on bool) error {
	scm, _, e := procOpenSCManager.Call(0, 0, scManagerConnect)
	if scm == 0 {
		return fmt.Errorf("service manager: %w", e)
	}
	defer procCloseServiceHandle.Call(scm)
	h, err := openService(scm, it.value, serviceQueryConfig|serviceChangeConfig)
	var origSDDL, sid string
	hardened := false
	if err != nil {
		if err != syscall.Errno(5) { // ERROR_ACCESS_DENIED
			return fmt.Errorf("open service %s: %w", it.value, err)
		}
		// The DACL denies everyone config writes — a deliberately hardened
		// service. Save its descriptor first; the enable path puts it back.
		var e2 error
		if origSDDL, e2 = serviceSDDL(scm, it.value); e2 != nil {
			return fmt.Errorf("security descriptor of %s: %w", it.value, e2)
		}
		hardened = true
	}

	start, delayed := uint32(serviceDisabled), false
	prev, hadPrev := recall(it.Key)
	if on {
		start = serviceAutoStart
		if hadPrev {
			start, delayed = prev.Start, prev.Delayed
		}
	} else if err := remember(it.Key, serviceState{Start: it.start, Delayed: it.delayed, SDDL: origSDDL}); err != nil {
		// Without a record Enable could not restore delayed start — refuse
		// rather than lose the original silently.
		return fmt.Errorf("could not save the original start type: %w", err)
	}
	if hardened {
		// Take ownership (the privilege grants WRITE_OWNER regardless of
		// the DACL), then append an ACE that lets our SID change config.
		if sid, err = currentUserSID(); err != nil {
			return err
		}
		if err := enableOwnerPrivileges(); err != nil {
			return fmt.Errorf("unlock %s: %w", it.value, err)
		}
		if err := takeServiceOwnership(scm, it.value, sid); err != nil {
			return fmt.Errorf("take ownership of %s: %w", it.value, err)
		}
		if err := grantSelfConfigControl(scm, it.value, sid, origSDDL); err != nil {
			return fmt.Errorf("grant change-config on %s: %w", it.value, err)
		}
		if h, err = openService(scm, it.value, serviceQueryConfig|serviceChangeConfig); err != nil {
			return fmt.Errorf("open service %s after unlock: %w", it.value, err)
		}
	}
	defer procCloseServiceHandle.Call(h)
	if r, _, e := procChangeServiceConfig.Call(h, serviceNoChange, uintptr(start), serviceNoChange, 0, 0, 0, 0, 0, 0, 0); r == 0 {
		return fmt.Errorf("change start type of %s: %w", it.value, e)
	}
	if on && start == serviceAutoStart {
		flag := uint32(0)
		if delayed {
			flag = 1
		}
		if r, _, e := procChangeServiceConfig2.Call(h, configDelayedAutoStart, uintptr(unsafe.Pointer(&flag))); r == 0 {
			return fmt.Errorf("restore delayed start of %s: %w", it.value, e)
		}
	}
	if on {
		// Put back the saved descriptor — the injected ACE and our
		// ownership both disappear. Saved when we hardened the path;
		// origSDDL covers a descriptor we read this call.
		sddl := prev.SDDL
		if sddl == "" {
			sddl = origSDDL
		}
		if sddl != "" {
			// WRITE_OWNER comes from the take-ownership privilege — a
			// fresh process has not enabled it yet. If enabling fails the
			// restore's own error still surfaces.
			_ = enableOwnerPrivileges()
			if err := restoreServiceSD(scm, it.value, sddl); err != nil {
				return fmt.Errorf("restore security of %s: %w", it.value, err)
			}
		}
		forget(it.Key)
	}
	return nil
}
