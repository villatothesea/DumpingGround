//go:build windows

package startup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf16"
)

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := []byte{0xFF, 0xFE}
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

func TestParseTask(t *testing.T) {
	const logon = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Author>Acme Corp</Author><Description>Keeps Acme current</Description></RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled></LogonTrigger>
    <TimeTrigger><Enabled>true</Enabled></TimeTrigger>
  </Triggers>
  <Settings><Enabled>false</Enabled></Settings>
  <Actions Context="Author"><Exec><Command>"C:\Program Files\Acme\updater.exe"</Command><Arguments>/silent</Arguments></Exec></Actions>
</Task>`
	task, trigger, ok := parseTask(utf16le(logon))
	if !ok || trigger != "At sign-in" {
		t.Fatalf("logon task: ok=%v trigger=%q", ok, trigger)
	}
	if enabledOr(task.Settings.Enabled) {
		t.Error("Settings/Enabled=false was read as enabled")
	}
	if task.Info.Author != "Acme Corp" || task.Actions.Exec[0].Arguments != "/silent" {
		t.Errorf("fields: %+v", task)
	}

	const boot = `<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><BootTrigger/><LogonTrigger><Enabled>false</Enabled></LogonTrigger></Triggers><Actions><ComHandler><ClassId>{ABC}</ClassId></ComHandler></Actions></Task>`
	task, trigger, ok = parseTask([]byte(boot))
	if !ok || trigger != "At startup" || !enabledOr(task.Settings.Enabled) || task.Actions.COM[0].ClassId != "{ABC}" {
		t.Fatalf("boot task: ok=%v trigger=%q %+v", ok, trigger, task)
	}

	const timer = `<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><TimeTrigger/></Triggers></Task>`
	if _, _, ok := parseTask([]byte(timer)); ok {
		t.Error("a time-triggered task is not a startup item")
	}
	if _, _, ok := parseTask([]byte("not xml")); ok {
		t.Error("garbage parsed")
	}
}

func TestSplitCommand(t *testing.T) {
	notepad := filepath.Join(os.Getenv("SystemRoot"), `System32\notepad.exe`)
	for _, tc := range []struct{ in, exe, args string }{
		{`"` + notepad + `" /a b`, notepad, "/a b"},
		{notepad + ` /a`, notepad, "/a"},
		{`%SystemRoot%\System32\notepad.exe -x`, notepad, "-x"},
		{`\??\` + notepad, notepad, ""},
		{``, "", ""},
	} {
		exe, args := splitCommand(tc.in)
		if !strings.EqualFold(exe, tc.exe) || args != tc.args {
			t.Errorf("splitCommand(%q) = %q, %q; want %q, %q", tc.in, exe, args, tc.exe, tc.args)
		}
	}
	if exe, args := splitCommand(`notepad.exe file.txt`); !strings.EqualFold(filepath.Base(exe), "notepad.exe") || !filepath.IsAbs(exe) || args != "file.txt" {
		t.Errorf("bare program name: %q, %q", exe, args)
	}
	if !underWindowsDir(notepad) || underWindowsDir(`C:\Program Files\x.exe`) {
		t.Error("underWindowsDir")
	}
}

func TestExpandEnv(t *testing.T) {
	os.Setenv("DG_TEST_VAR", "value")
	for in, want := range map[string]string{
		"%DG_TEST_VAR%\\x": "value\\x", "a%UNSET_DG_VAR%b": "a%UNSET_DG_VAR%b", "100%": "100%", "%%": "%%", "plain": "plain",
	} {
		if got := expandEnv(in); got != want {
			t.Errorf("expandEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFileInfo(t *testing.T) {
	vi := fileInfo(filepath.Join(os.Getenv("SystemRoot"), `System32\notepad.exe`))
	if !strings.Contains(vi.Company, "Microsoft") || vi.Desc == "" {
		t.Errorf("notepad version info: %+v", vi)
	}
}

// TestLive lists this machine's startup items; run with -v to read them.
func TestLive(t *testing.T) {
	items := List()
	counts := map[Kind]int{}
	for _, it := range items {
		counts[it.Kind]++
		if it.Key == "" || it.Name == "" {
			t.Errorf("item without key or name: %+v", it)
		}
		if !it.Windows || testing.Verbose() && it.Kind != KindTask {
			t.Logf("%-14s %-8v run=%-5v win=%-5v %-34.34q %-24.24q %s", it.Kind, it.Enabled, it.Running, it.Windows, it.Name, it.Publisher, it.Command)
		}
	}
	t.Logf("apps=%d folder=%d services=%d tasks=%d", counts[KindRun], counts[KindFolder], counts[KindService], counts[KindTask])
	if counts[KindService] == 0 {
		t.Error("no automatic services found")
	}
}

func regDeleteValue(t *testing.T, hive, path, name string) {
	exec.Command("reg", "delete", hive+`\`+path, "/v", name, "/f").Run()
}

func find(items []Item, key string) *Item {
	for i := range items {
		if items[i].Key == key {
			return &items[i]
		}
	}
	return nil
}

// TestRunItemRoundTrip creates a real Run value in HKCU, switches it off
// and on through StartupApproved, and checks the Run value itself is never
// touched — the property that makes disabling reversible.
func TestRunItemRoundTrip(t *testing.T) {
	const name = "DumpingGroundTestItem"
	notepad := filepath.Join(os.Getenv("SystemRoot"), `System32\notepad.exe`)
	if out, err := exec.Command("reg", "add", `HKCU\`+runPath, "/v", name, "/t", "REG_SZ", "/d", `"`+notepad+`" /dgtest`, "/f").CombinedOutput(); err != nil {
		t.Skipf("cannot write HKCU Run: %v %s", err, out)
	}
	defer regDeleteValue(t, "HKCU", runPath, name)
	defer regDeleteValue(t, "HKCU", approvedPath+"Run", name)

	key := "run|HKCU|Run|" + name
	it := find(List(), key)
	if it == nil || !it.Enabled || !strings.EqualFold(it.Exe, notepad) || it.Locked != "" || it.Scope != "Current user" {
		t.Fatalf("fresh item: %+v", it)
	}
	if err := SetEnabled(*it, false); err != nil {
		t.Fatal(err)
	}
	it = find(List(), key)
	if it == nil || it.Enabled {
		t.Fatalf("still enabled after disabling: %+v", it)
	}
	if !approvedDisabled("HKCU", "Run", name) {
		t.Error("StartupApproved flag not written")
	}
	// The Run value is untouched: disabling is not deleting.
	h, err := openKey(syscall.HKEY_CURRENT_USER, runPath, syscall.KEY_QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if v := queryValue(h, name); len(v) == 0 {
		t.Error("the Run value disappeared when the item was disabled")
	}
	syscall.RegCloseKey(h)
	if err := SetEnabled(*it, true); err != nil {
		t.Fatal(err)
	}
	if it = find(List(), key); it == nil || !it.Enabled || approvedDisabled("HKCU", "Run", name) {
		t.Fatalf("not enabled again: %+v", it)
	}
}

// TestFolderItemRoundTrip does the same for a file in the user's Startup
// folder.
func TestFolderItemRoundTrip(t *testing.T) {
	dir := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Startup`)
	file := filepath.Join(dir, "dumpingground-test.cmd")
	if err := os.WriteFile(file, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Skip(err)
	}
	defer os.Remove(file)
	defer regDeleteValue(t, "HKCU", approvedPath+"StartupFolder", "dumpingground-test.cmd")
	key := "folder|HKCU|dumpingground-test.cmd"
	it := find(List(), key)
	if it == nil || !it.Enabled || it.Name != "dumpingground-test" {
		t.Fatalf("fresh item: %+v", it)
	}
	if err := SetEnabled(*it, false); err != nil {
		t.Fatal(err)
	}
	if it = find(List(), key); it == nil || it.Enabled {
		t.Fatalf("not disabled: %+v", it)
	}
	if _, err := os.Stat(file); err != nil {
		t.Error("the file was removed when the item was disabled")
	}
	if err := SetEnabled(*it, true); err != nil {
		t.Fatal(err)
	}
	if it = find(List(), key); it == nil || !it.Enabled {
		t.Fatalf("not enabled again: %+v", it)
	}
}

func TestLockedItemsRefuse(t *testing.T) {
	if err := SetEnabled(Item{Kind: KindService, Name: "RpcSs", Locked: windowsLock}, false); err == nil {
		t.Fatal("a locked item was switched")
	}
}

func TestUndoStoreKeepsFirstOriginal(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("LOCALAPPDATA", dir)
	defer os.Unsetenv("LOCALAPPDATA")
	if err := remember("service|X", serviceState{Start: 2, Delayed: true}); err != nil {
		t.Skip(err)
	}
	// A second disable sees "disabled" — it must not overwrite the record.
	remember("service|X", serviceState{Start: 4})
	s, ok := recall("service|X")
	if !ok || s.Start != 2 || !s.Delayed {
		t.Fatalf("recall = %+v, %v", s, ok)
	}
	forget("service|X")
	if _, ok := recall("service|X"); ok {
		t.Fatal("record survived forget")
	}
}
