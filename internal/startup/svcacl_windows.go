//go:build windows

package startup

import (
	"encoding/binary"
	"fmt"
	"os/user"
	"strings"
	"syscall"
	"unsafe"
)

// Some packaged services (CodexSandboxService.OpenAI.Codex is the one we
// met) harden their security descriptor: the DACL hands Authenticated
// Users query/start/stop but grants change-config to nobody, so even an
// elevated OpenService(SERVICE_CHANGE_CONFIG) is refused. The documented
// way out is the take-ownership privilege: it grants WRITE_OWNER
// regardless of the DACL, the new owner then has implicit WRITE_DAC, and
// an ACE giving our SID change-config opens the door. The original
// descriptor is saved in the undo record before anything is touched —
// restoring a service puts owner and DACL back byte for byte, so the
// hardening itself is left exactly as it was found.

var (
	procQuerySvcSecurity = advapi32.NewProc("QueryServiceObjectSecurity")
	procSetSvcSecurity   = advapi32.NewProc("SetServiceObjectSecurity")
	procSDToSDDL         = advapi32.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW")
	procSDDLToSD         = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procOpenProcToken    = advapi32.NewProc("OpenProcessToken")
	procLookupPrivValue  = advapi32.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivs = advapi32.NewProc("AdjustTokenPrivileges")
	procLocalFree        = kernel32.NewProc("LocalFree")
)

const (
	svcReadControl  = 0x20000
	svcWriteDAC     = 0x40000
	svcWriteOwner   = 0x80000
	ownerSecInfo    = 0x1 // OWNER_SECURITY_INFORMATION
	daclSecInfo     = 0x4 // DACL_SECURITY_INFORMATION
	tokenQuery      = 0x8
	tokenAdjustPriv = 0x20
	privEnabled     = 0x2 // SE_PRIVILEGE_ENABLED
)

// openService opens a service handle with exactly the access asked for.
func openService(scm uintptr, name string, access uint32) (uintptr, error) {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	h, _, e := procOpenService.Call(scm, uintptr(unsafe.Pointer(n)), uintptr(access))
	if h == 0 {
		return 0, e
	}
	return h, nil
}

// serviceSDDL reads the service object's security descriptor as SDDL
// (owner + DACL). READ_CONTROL survives even hardening — everyone gets
// to look, just not touch.
func serviceSDDL(scm uintptr, name string) (string, error) {
	h, err := openService(scm, name, svcReadControl)
	if err != nil {
		return "", err
	}
	defer procCloseServiceHandle.Call(h)
	var need uint32
	procQuerySvcSecurity.Call(h, ownerSecInfo|daclSecInfo, 0, 0, uintptr(unsafe.Pointer(&need)))
	if need == 0 || need > 64<<10 {
		return "", fmt.Errorf("security descriptor size %d", need)
	}
	buf := make([]byte, need)
	if r, _, e := procQuerySvcSecurity.Call(h, ownerSecInfo|daclSecInfo, uintptr(unsafe.Pointer(&buf[0])), uintptr(need), uintptr(unsafe.Pointer(&need))); r == 0 {
		return "", e
	}
	var sddl *uint16
	if r, _, e := procSDToSDDL.Call(uintptr(unsafe.Pointer(&buf[0])), 1, ownerSecInfo|daclSecInfo, uintptr(unsafe.Pointer(&sddl)), 0); r == 0 {
		return "", e
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(sddl)))
	return utf16Ptr(sddl), nil
}

// utf16Ptr reads a NUL-terminated UTF-16 string from an API-owned buffer.
func utf16Ptr(p *uint16) string {
	n := 0
	for *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + uintptr(2*n))) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

// sddlToSD turns an SDDL string into a security descriptor. The returned
// pointer is LocalFree'd by the caller.
func sddlToSD(sddl string) (uintptr, error) {
	p, err := syscall.UTF16PtrFromString(sddl)
	if err != nil {
		return 0, err
	}
	var sd uintptr
	if r, _, e := procSDDLToSD.Call(uintptr(unsafe.Pointer(p)), 1, uintptr(unsafe.Pointer(&sd)), 0); r == 0 {
		return 0, e
	}
	return sd, nil
}

// currentUserSID is the caller's own SID — the SID the injected ACE
// grants change-config to. os/user reports the SID string on Windows.
func currentUserSID() (string, error) {
	u, err := user.Current()
	if err != nil || u.Uid == "" {
		return "", fmt.Errorf("current user SID: %v", err)
	}
	return u.Uid, nil
}

// enableOwnerPrivileges switches on the two privileges the hardened
// path needs: SeTakeOwnershipPrivilege grants WRITE_OWNER no matter the
// DACL, SeRestorePrivilege lets us hand the owner field back to SYSTEM
// — without it an admin may only assign owners it owns. Both stay on
// for the process lifetime.
func enableOwnerPrivileges() error {
	var tok uintptr
	if r, _, e := procOpenProcToken.Call(^uintptr(0), tokenAdjustPriv|tokenQuery, uintptr(unsafe.Pointer(&tok))); r == 0 {
		return fmt.Errorf("open process token: %w", e)
	}
	defer syscall.CloseHandle(syscall.Handle(tok))
	for _, priv := range []string{"SeTakeOwnershipPrivilege", "SeRestorePrivilege"} {
		name, _ := syscall.UTF16PtrFromString(priv)
		var luid uint64
		if r, _, e := procLookupPrivValue.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&luid))); r == 0 {
			return fmt.Errorf("lookup %s: %w", priv, e)
		}
		var tp [16]byte // TOKEN_PRIVILEGES: count + one LUID_AND_ATTRIBUTES
		binary.LittleEndian.PutUint32(tp[:], 1)
		binary.LittleEndian.PutUint64(tp[4:], luid)
		binary.LittleEndian.PutUint32(tp[12:], privEnabled)
		if r, _, e := procAdjustTokenPrivs.Call(tok, 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0); r == 0 {
			return fmt.Errorf("enable %s: %w", priv, e)
		}
	}
	return nil
}

// takeServiceOwnership makes sid the owner of the service object. With
// the take-ownership privilege enabled the OS grants WRITE_OWNER no
// matter what the DACL says.
func takeServiceOwnership(scm uintptr, name, sid string) error {
	h, err := openService(scm, name, svcWriteOwner|svcReadControl)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(h)
	sd, err := sddlToSD("O:" + sid)
	if err != nil {
		return err
	}
	defer procLocalFree.Call(sd)
	if r, _, e := procSetSvcSecurity.Call(h, ownerSecInfo, sd); r == 0 {
		return e
	}
	return nil
}

// grantSelfConfigControl appends (A;;DC;;;sid) — change-config for us —
// to the service's DACL. WRITE_DAC is implicit for the owner, so this
// only works after takeServiceOwnership. The original DACL is kept whole
// and ours is appended; the full descriptor is restored on re-enable.
func grantSelfConfigControl(scm uintptr, name, sid, orig string) error {
	if strings.Contains(orig, "(A;;DC;;;"+sid+")") {
		return nil // already unlocked by an earlier pass
	}
	h, err := openService(scm, name, svcWriteDAC)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(h)
	// The ACE belongs inside the D: section — append at the end, or just
	// before a trailing SACL if one exists.
	sddl := orig + "(A;;DC;;;" + sid + ")"
	if i := strings.LastIndex(orig, "S:"); i >= 0 {
		sddl = orig[:i] + "(A;;DC;;;" + sid + ")" + orig[i:]
	}
	sd, err := sddlToSD(sddl)
	if err != nil {
		return err
	}
	defer procLocalFree.Call(sd)
	if r, _, e := procSetSvcSecurity.Call(h, daclSecInfo, sd); r == 0 {
		return e
	}
	return nil
}

// restoreServiceSD writes back the saved owner+DACL — our injected ACE
// and the ownership change disappear together, leaving the service's
// hardening exactly as it was found.
func restoreServiceSD(scm uintptr, name, sddl string) error {
	h, err := openService(scm, name, svcWriteOwner|svcWriteDAC)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(h)
	sd, err := sddlToSD(sddl)
	if err != nil {
		return err
	}
	defer procLocalFree.Call(sd)
	if r, _, e := procSetSvcSecurity.Call(h, ownerSecInfo|daclSecInfo, sd); r == 0 {
		return e
	}
	return nil
}
