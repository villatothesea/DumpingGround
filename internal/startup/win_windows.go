//go:build windows

package startup

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	versionD = syscall.NewLazyDLL("version.dll")

	procK32EnumProcesses       = kernel32.NewProc("K32EnumProcesses")
	procOpenProcess            = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImage  = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetFileVersionInfoSize = versionD.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfo     = versionD.NewProc("GetFileVersionInfoW")
	procVerQueryValue          = versionD.NewProc("VerQueryValueW")
)

func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func le64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

// utf16z decodes little-endian UTF-16 up to the first NUL.
func utf16z(b []byte) string {
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

// expandEnv expands %VAR% references; unknown names are left as written.
func expandEnv(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '%')
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		if v, ok := os.LookupEnv(s[i+1 : i+1+j]); ok && j > 0 {
			b.WriteString(s[:i])
			b.WriteString(v)
			s = s[i+j+2:]
			continue
		}
		b.WriteString(s[:i+1])
		s = s[i+1:]
	}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// splitCommand separates the program from its arguments the way Windows
// does: a quoted path is exact; an unquoted one with spaces is the
// shortest prefix that names a file ("C:\Program Files\X\x.exe /s").
func splitCommand(cmd string) (exe, args string) {
	cmd = strings.TrimSpace(expandEnv(cmd))
	cmd = strings.TrimPrefix(cmd, `\??\`)
	if cmd == "" {
		return "", ""
	}
	if cmd[0] == '"' {
		if i := strings.IndexByte(cmd[1:], '"'); i >= 0 {
			exe, args = resolveExe(cmd[1:1+i]), strings.TrimSpace(cmd[2+i:])
		} else {
			exe = resolveExe(strings.Trim(cmd, `"`))
		}
		return lookThroughLauncher(exe, args)
	}
	for i := 0; i <= len(cmd); i++ {
		if i < len(cmd) && cmd[i] != ' ' {
			continue
		}
		if r := resolveExe(cmd[:i]); filepath.IsAbs(r) && fileExists(r) {
			if i == len(cmd) {
				return lookThroughLauncher(r, "")
			}
			return lookThroughLauncher(r, strings.TrimSpace(cmd[i+1:]))
		}
	}
	first, rest, _ := strings.Cut(cmd, " ")
	return resolveExe(first), strings.TrimSpace(rest)
}

// lookThroughLauncher returns the library a rundll32 command loads — that
// is whose software it is; anything else comes back unchanged.
func lookThroughLauncher(exe, args string) (string, string) {
	if !strings.EqualFold(filepath.Base(exe), "rundll32.exe") || args == "" {
		return exe, args
	}
	target, _, _ := strings.Cut(strings.TrimSpace(args), ",")
	target = strings.Trim(strings.TrimSpace(target), `"`)
	if lib := resolveExe(target); filepath.IsAbs(lib) && fileExists(lib) {
		return lib, args
	}
	return exe, args
}

// resolveExe turns a bare or relative program name into a full path when
// it can: System32-relative service paths and PATH lookups (rundll32,
// cmd).
func resolveExe(p string) string {
	if filepath.IsAbs(p) {
		if !fileExists(p) && fileExists(p+".exe") {
			return p + ".exe"
		}
		return p
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		if c := filepath.Join(root, p); fileExists(c) {
			return c
		}
	}
	if full, err := exec.LookPath(p); err == nil {
		return full
	}
	return p
}

// underWindowsDir reports whether p is inside the Windows directory.
func underWindowsDir(p string) bool {
	root := os.Getenv("SystemRoot")
	return root != "" && p != "" && strings.HasPrefix(strings.ToLower(p), strings.ToLower(root)+`\`)
}

// ---- version resource ----

type verInfo struct{ Company, Desc, Product string }

var verCache sync.Map // lower-case path → verInfo

// fileInfo reads a file's version resource: who made it and what it is.
func fileInfo(path string) verInfo {
	key := strings.ToLower(path)
	if v, ok := verCache.Load(key); ok {
		return v.(verInfo)
	}
	v := readVersion(path)
	verCache.Store(key, v)
	return v
}

func readVersion(path string) (vi verInfo) {
	if !filepath.IsAbs(path) {
		return
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	size, _, _ := procGetFileVersionInfoSize.Call(uintptr(unsafe.Pointer(p)), 0)
	if size == 0 || size > 8<<20 {
		return
	}
	buf := make([]byte, size)
	if r, _, _ := procGetFileVersionInfo.Call(uintptr(unsafe.Pointer(p)), 0, size, uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return
	}
	base := uintptr(unsafe.Pointer(&buf[0]))
	// VerQueryValue hands back a pointer into buf; turn it into an offset
	// instead of dereferencing it.
	query := func(sub string, unit int) []byte {
		q, err := syscall.UTF16PtrFromString(sub)
		if err != nil {
			return nil
		}
		var ptr uintptr
		var n uint32
		if r, _, _ := procVerQueryValue.Call(base, uintptr(unsafe.Pointer(q)), uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&n))); r == 0 || n == 0 {
			return nil
		}
		off, end := int(ptr-base), int(ptr-base)+int(n)*unit
		if off < 0 || end > len(buf) {
			return nil
		}
		return buf[off:end]
	}
	langs := []string{"040904B0", "040904E4", "04090000"}
	if tr := query(`\VarFileInfo\Translation`, 1); len(tr) >= 4 {
		langs = append([]string{fmt.Sprintf("%04X%04X", binary.LittleEndian.Uint16(tr), binary.LittleEndian.Uint16(tr[2:]))}, langs...)
	}
	get := func(field string) string {
		for _, l := range langs {
			if b := query(`\StringFileInfo\`+l+`\`+field, 2); b != nil {
				if s := strings.TrimSpace(utf16z(b)); s != "" {
					return strings.Join(strings.Fields(s), " ")
				}
			}
		}
		return ""
	}
	return verInfo{Company: get("CompanyName"), Desc: get("FileDescription"), Product: get("ProductName")}
}

// ---- running programs ----

// runningImages returns the lower-cased image paths of every process this
// session may query.
func runningImages() map[string]bool {
	out := map[string]bool{}
	pids := make([]uint32, 4096)
	var needed uint32
	if r, _, _ := procK32EnumProcesses.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)*4), uintptr(unsafe.Pointer(&needed))); r == 0 {
		return out
	}
	buf := make([]uint16, 1024)
	for _, pid := range pids[:min(int(needed)/4, len(pids))] {
		if pid == 0 {
			continue
		}
		h, _, _ := procOpenProcess.Call(0x1000, 0, uintptr(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
		if h == 0 {
			continue
		}
		n := uint32(len(buf))
		if r, _, _ := procQueryFullProcessImage.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r != 0 {
			out[strings.ToLower(syscall.UTF16ToString(buf[:n]))] = true
		}
		syscall.CloseHandle(syscall.Handle(h))
	}
	return out
}
