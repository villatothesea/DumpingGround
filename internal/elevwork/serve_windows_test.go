//go:build windows

package elevwork

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/dumpingground/dumpingground/internal/elevate"
)

// pipeClient is the raw client side of the worker protocol — enough to
// exercise the handshake without pulling in package elevate's globals.
func pipeClient(t *testing.T, pipe string) *os.File {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(pipe)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
		if err == nil {
			return os.NewFile(uintptr(h), pipe)
		}
		if time.Now().After(deadline) {
			t.Fatalf("pipe %s never came up: %v", pipe, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func frame(t *testing.T, f *os.File, req *elevate.Request) elevate.Response {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := f.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(body); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, binary.LittleEndian.Uint32(hdr[:]))
	if _, err := io.ReadFull(f, buf); err != nil {
		t.Fatal(err)
	}
	var resp elevate.Response
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestServeRoundTrip runs the real worker loop in-process: the pipe
// comes up, our own PID passes authentication, ping and shutdown
// answer, and a refused op reports its error rather than dying.
func TestServeRoundTrip(t *testing.T) {
	pipe := fmt.Sprintf(`\\.\pipe\dg-elevtest-%d-%d`, os.Getpid(), time.Now().UnixNano())
	done := make(chan int, 1)
	go func() { done <- Serve(uint32(os.Getpid()), pipe) }()
	f := pipeClient(t, pipe)

	if r := frame(t, f, &elevate.Request{Op: elevate.OpPing}); !r.OK {
		t.Fatalf("ping failed: %s", r.Err)
	}
	if r := frame(t, f, &elevate.Request{Op: elevate.OpRegDelete, Key: `SYSTEM\CurrentControlSet`}); r.OK {
		t.Fatal("off-whitelist registry delete was allowed")
	}
	if r := frame(t, f, &elevate.Request{Op: elevate.OpDeletePath, Path: `C:\Windows`}); r.OK {
		t.Fatal("deleting C:\\Windows was allowed")
	}
	if r := frame(t, f, &elevate.Request{Op: elevate.OpOpenVolume, Drive: "💣"}); r.OK {
		t.Fatal("bogus drive was allowed")
	}
	if r := frame(t, f, &elevate.Request{Op: elevate.OpShutdown}); !r.OK {
		t.Fatalf("shutdown failed: %s", r.Err)
	}
	f.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("Serve exit code %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not exit after shutdown")
	}
}

// TestServeRejectsForeignClient: a client whose PID is not the declared
// parent gets disconnected — the worker then waits for the real one.
func TestServeRejectsForeignClient(t *testing.T) {
	ppid := uint32(os.Getppid()) // alive, but not our image and not us
	if ppid == uint32(os.Getpid()) {
		t.Skip("ppid == pid?!")
	}
	pipe := fmt.Sprintf(`\\.\pipe\dg-elevtest-rej-%d-%d`, os.Getpid(), time.Now().UnixNano())
	go Serve(ppid, pipe)
	f := pipeClient(t, pipe)
	// The rejection disconnects us — the first write or read must fail.
	f.Write([]byte{4, 0, 0, 0})
	buf := make([]byte, 4)
	_ = f.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(f, buf); err == nil {
		t.Error("foreign client was not disconnected")
	}
	f.Close()
}

// TestServeBogusParent: an unrunnable parent id ends the worker before
// the pipe is ever created.
func TestServeBogusParent(t *testing.T) {
	pipe := fmt.Sprintf(`\\.\pipe\dg-elevtest-bogus-%d`, time.Now().UnixNano())
	if code := Serve(0x7fffffff, pipe); code == 0 {
		t.Fatal("Serve accepted a nonexistent parent")
	}
}

func TestRegAllowed(t *testing.T) {
	cases := []struct {
		key     string
		valueOp bool
		want    bool
	}{
		// Uninstall entries — both views.
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\SomeApp`, false, true},
		{`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\SomeApp`, false, true},
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\SomeApp`, true, true},
		// Startup list values live directly on the list root.
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\Run`, true, true},
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\RunOnce`, true, true},
		// Deleting the whole Run key is never legitimate.
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\Run`, false, false},
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, false, false},
		// Vendor trees are fair game, including their root key.
		{`SOFTWARE\Valve`, false, true},
		{`SOFTWARE\Valve\Steam`, true, true},
		{`SOFTWARE\WOW6432Node\Valve`, false, true},
		// Anything Microsoft outside the lists is refused — including
		// the bare Microsoft key itself.
		{`SOFTWARE\Microsoft`, false, false},
		{`SOFTWARE\Microsoft\Cryptography`, false, false},
		{`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, true, false},
		{`SOFTWARE\Classes`, false, false},
		{`SOFTWARE\Classes\CLSID\x`, true, false},
		{`SOFTWARE\Policies\Microsoft`, false, false},
		// Everything outside SOFTWARE is refused.
		{`SYSTEM\CurrentControlSet\Services`, false, false},
		{`SOFTWARE`, false, false},
		{`SECURITY\SAM`, false, false},
		// Prefix tricks: "RUNX" must not inherit the Run allow.
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\RunEx`, false, false},
		{`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstallable`, false, false},
	}
	for _, tc := range cases {
		if got := regAllowed(tc.key, tc.valueOp); got != tc.want {
			t.Errorf("regAllowed(%q, valueOp=%v) = %v, want %v", tc.key, tc.valueOp, got, tc.want)
		}
	}
}

func TestSafeTarget(t *testing.T) {
	ok := []string{
		`C:\Windows\Temp`,
		`C:\Windows\SoftwareDistribution\Download`,
		`C:\Windows\Minidump\dump.dmp`,
		`C:\Windows.old\Windows\Temp`,
		`C:\Program Files\SomeApp`,
		`C:\Program Files (x86)\SomeApp\subdir`,
		`C:\ProgramData\SomeApp`,
		`C:\Users\me\Downloads\file.tmp`,
		`C:\Users\me\AppData\Local\Temp\x`,
		`D:\scrap\thing`,
		`E:\a`,
	}
	for _, p := range ok {
		if !safeTarget(p) {
			t.Errorf("safeTarget(%q) = false, want true", p)
		}
	}
	bad := []string{
		``, `C:\`, `C:`, `x`, `relative\path`,
		`\\server\share\dir`,
		`C:\Windows`, `C:\Windows\System32`, `C:\Windows\System32\drivers`,
		`C:\Windows\WinSxS\x`, `C:\Windows\Fonts\f.ttf`,
		`C:\Users`, `C:\Users\me`, `C:\Users\me\Downloads`, `C:\Users\me\Desktop`,
		`C:\Program Files`, `C:\ProgramData`, `C:\$Recycle.Bin`,
		`C:\PerfLogs\x`,
		`C:\Windows\*`,
		`C:\Windows\Temp\a<b`,
	}
	for _, p := range bad {
		if safeTarget(p) {
			t.Errorf("safeTarget(%q) = true, want false", p)
		}
	}
}

func TestAnyReparse(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	inside := filepath.Join(real, "inside")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "junction")
	// Directory junctions need no privilege, unlike symlinks.
	if err := exec.Command("cmd", "/c", "mklink", "/J", junction, real).Run(); err != nil {
		t.Skipf("mklink unavailable: %v", err)
	}
	if anyReparse(inside) {
		t.Error("anyReparse on a plain path = true")
	}
	if !anyReparse(filepath.Join(junction, "inside")) {
		t.Error("anyReparse through a junction = false, want true")
	}
	if !anyReparse(junction) {
		t.Error("anyReparse on the junction itself = false, want true")
	}
}
