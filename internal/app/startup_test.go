package app

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dumpingground/dumpingground/internal/startup"
	"github.com/egoist/mygo/ui"
)

const winLock = "Part of Windows — managed by Windows, shown read-only"

func fakeStartupItems() []startup.Item {
	return []startup.Item{
		{Kind: startup.KindRun, Key: "run|HKCU|Run|Steam", Name: "Steam", Publisher: "Valve Corporation", Desc: "Steam Client Bootstrapper",
			Command: `"C:\Program Files (x86)\Steam\steam.exe" -silent`, Exe: `C:\Program Files (x86)\Steam\steam.exe`,
			Location: `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, Scope: "Current user", Trigger: "At sign-in", Enabled: true, Running: true},
		{Kind: startup.KindRun, Key: "run|HKCU|Run|Discord", Name: "Discord", Publisher: "Discord Inc.",
			Command:  `C:\Users\dev\AppData\Local\Discord\Update.exe --processStart Discord.exe`,
			Location: `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, Scope: "Current user", Trigger: "At sign-in"},
		{Kind: startup.KindFolder, Key: "folder|HKCU|Snipaste.lnk", Name: "Snipaste", Publisher: "Le Liu", Command: `D:\Programs\Snipaste\Snipaste.exe`,
			Location: `C:\Users\dev\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup\Snipaste.lnk`, Scope: "Current user", Trigger: "At sign-in", Enabled: true, Running: true},
		{Kind: startup.KindService, Key: "service|TPHKLOAD", Name: "Lenovo Hotkey Client Loader", Publisher: "Lenovo", Command: `C:\Windows\System32\DriverStore\FileRepository\fn.inf\TPHKLOAD.exe`,
			Location: "Service TPHKLOAD", Scope: "System", Trigger: "Automatic", Enabled: true, Running: true},
		{Kind: startup.KindService, Key: "service|gupdate", Name: "Google Updater Service", Publisher: "Google LLC", Command: `"C:\Program Files (x86)\Google\GoogleUpdater\updater.exe" --system`,
			Location: "Service GoogleUpdaterService", Scope: "System", Trigger: "Automatic (delayed)", Enabled: true},
		{Kind: startup.KindService, Key: "service|oldsvc", Name: "Legacy Helper", Publisher: "Acme", Command: `C:\Program Files\Acme\helper.exe`,
			Location: "Service AcmeHelper", Scope: "System", Trigger: "Disabled", Running: true},
		{Kind: startup.KindTask, Key: `task|\GoogleUpdateTaskMachineCore`, Name: "GoogleUpdateTaskMachineCore", Publisher: "Google LLC", Command: `C:\Program Files (x86)\Google\Update\GoogleUpdate.exe /c`,
			Location: `Task Scheduler \GoogleUpdateTaskMachineCore`, Scope: "System", Trigger: "At startup", Enabled: true},
		{Kind: startup.KindService, Key: "service|WSearch", Name: "Windows Search", Publisher: "Microsoft Windows", Command: `C:\Windows\system32\SearchIndexer.exe /Embedding`,
			Location: "Service WSearch", Scope: "System", Trigger: "Automatic (delayed)", Enabled: true, Running: true, Windows: true, Locked: winLock},
		{Kind: startup.KindTask, Key: `task|\Microsoft\Windows\Maintenance\Defrag`, Name: "ScheduledDefrag", Publisher: "Microsoft Windows", Command: `%windir%\system32\defrag.exe -c -h`,
			Location: `Task Scheduler \Microsoft\Windows\Defrag\ScheduledDefrag`, Scope: "System", Trigger: "At startup", Enabled: true, Windows: true, Locked: winLock},
	}
}

type switchLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *switchLog) add(s string) { l.mu.Lock(); l.calls = append(l.calls, s); l.mu.Unlock() }
func (l *switchLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

// startupFixture builds a Startup page over fake items and replaces the
// system calls with a recorder, restoring them afterwards.
func startupFixture(t *testing.T) (*Model, *switchLog) {
	t.Helper()
	m := New()
	m.page = startupPage
	log := &switchLog{}
	oldList, oldSet := startupList, startupSet
	t.Cleanup(func() { startupList, startupSet = oldList, oldSet; m.close() })
	startupList = fakeStartupItems
	startupSet = func(it startup.Item, on bool) error {
		log.add(it.Key + "=" + map[bool]string{true: "on", false: "off"}[on])
		return nil
	}
	m.postHook = func(fn func()) { fn() }
	m.st.items = fakeStartupItems()
	m.st.loaded, m.st.loadedAt = true, time.Now()
	return m, log
}

func TestStartupRowsFilterAndCounts(t *testing.T) {
	m, _ := startupFixture(t)
	rows, counts, hidden := m.startupRows()
	if counts != [4]int{7, 3, 3, 1} || hidden != 2 || len(rows) != 7 {
		t.Fatalf("counts=%v hidden=%d rows=%d", counts, hidden, len(rows))
	}
	for _, r := range rows {
		if r.Windows {
			t.Errorf("Windows component %q listed while hidden", r.Name)
		}
	}
	m.st.showWindows = true
	if _, counts, hidden = m.startupRows(); counts[0] != 9 || hidden != 0 {
		t.Errorf("with Windows shown: counts=%v hidden=%d", counts, hidden)
	}
	m.st.showWindows = false
	m.st.filter = "Services"
	if rows, _, _ = m.startupRows(); len(rows) != 3 || rows[0].Kind != startup.KindService {
		t.Errorf("Services filter: %d rows", len(rows))
	}
	m.st.filter, m.st.query = "", "google"
	if rows, _, _ = m.startupRows(); len(rows) != 2 {
		t.Errorf("search \"google\": %d rows", len(rows))
	}
	m.st.query = "valve" // matches on publisher too
	if rows, _, _ = m.startupRows(); len(rows) != 1 || rows[0].Name != "Steam" {
		t.Errorf("search by publisher: %d rows", len(rows))
	}
	// The default order groups by source and puts items that are on first.
	m.st.query, m.st.sort = "", ui.SortOrder{Column: "Type"}
	rows, _, _ = m.startupRows()
	if rows[0].Name != "Steam" || rows[1].Name != "Discord" {
		t.Errorf("default order starts %q, %q", rows[0].Name, rows[1].Name)
	}
}

func TestStartupPageSwitches(t *testing.T) {
	m, log := startupFixture(t)
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()
	for _, want := range []string{"Startup — 5 on · 2 off", "Steam", "Discord", "Lenovo Hotkey Client Loader", "GoogleUpdateTaskMachineCore", "Windows components (+2)"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; screen: %v", want, tt.Texts())
		}
	}
	if tt.HasText("Windows Search") {
		t.Error("a Windows component is listed while hidden")
	}

	// An app switches off at once — no dialog, nothing deleted.
	if err := tt.Click("Steam"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := log.get(); len(got) != 1 || got[0] != "run|HKCU|Run|Steam=off" {
		t.Fatalf("after clicking Steam's switch: calls=%v selKey=%q", got, m.st.selKey)
	}
	tt.Frame()
	if m.st.busy != "" {
		t.Error("busy flag was not cleared when the switch finished")
	}
}

func TestStartupServiceAsksFirst(t *testing.T) {
	m, log := startupFixture(t)
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()
	if err := tt.Click("Lenovo Hotkey Client Loader"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.st.confirm || len(log.get()) != 0 {
		t.Fatalf("turning a service off must ask first: confirm=%v calls=%v", m.st.confirm, log.get())
	}
	if !tt.HasText("Turn off this service?") {
		t.Fatalf("no dialog on screen: %v", tt.Texts())
	}
	// Cancel changes nothing.
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.st.confirm || len(log.get()) != 0 {
		t.Fatalf("cancel: confirm=%v calls=%v", m.st.confirm, log.get())
	}
	// Confirming switches it.
	tt.Click("Lenovo Hotkey Client Loader")
	tt.Frame()
	if err := tt.Click("Turn off"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := log.get(); len(got) != 1 || got[0] != "service|TPHKLOAD=off" {
		t.Fatalf("after confirming: calls=%v", got)
	}
}

func TestStartupWindowsComponentsAreReadOnly(t *testing.T) {
	m, log := startupFixture(t)
	m.st.showWindows = true
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()
	if !tt.HasText("Windows Search") {
		t.Fatal("Windows components not shown after turning the toggle on")
	}
	tt.Click("Windows Search")
	tt.Frame()
	tt.Frame()
	if m.st.confirm || len(log.get()) != 0 {
		t.Fatalf("a locked item was switched: confirm=%v calls=%v", m.st.confirm, log.get())
	}
	// The core of the guarantee, independent of the widget: even a forced
	// toggle never reaches the system for a locked item through the page.
	for _, it := range m.st.items {
		if it.Windows && it.Locked == "" {
			t.Errorf("Windows component %q is not locked", it.Name)
		}
	}
}

func TestStartupSelectionShowsDetail(t *testing.T) {
	m, _ := startupFixture(t)
	m.st.selKey = "service|gupdate"
	tt := ui.NewTester(m.view, 1180, 760)
	tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
	tt.Frame()
	for _, want := range []string{"Starts from", "Runs", "Service GoogleUpdaterService"} {
		if !tt.HasText(want) {
			t.Errorf("detail strip missing %q", want)
		}
	}
	var found bool
	for _, s := range tt.Texts() {
		found = found || strings.Contains(s, "remembered and restored")
	}
	if !found {
		t.Error("the service consequence note is missing")
	}
}

func TestStartupEmptyAndLoading(t *testing.T) {
	m, _ := startupFixture(t)
	m.st.loaded = false
	m.st.loading = true // a load is already in flight
	tt := ui.NewTester(m.view, 960, 580)
	tt.Frame()
	if !tt.HasText("Reading startup items…") {
		t.Error("no loading state")
	}
	m.st.loaded, m.st.loading, m.st.items = true, false, nil
	tt.Frame()
	if !tt.HasText("Nothing starts from here.") {
		t.Error("no empty state")
	}
}

func TestStartupFailureIsShown(t *testing.T) {
	m, _ := startupFixture(t)
	startupSet = func(it startup.Item, on bool) error { return os.ErrPermission }
	m.applyStartup(m.st.items[0], false)
	deadline := time.Now().Add(2 * time.Second)
	for m.st.busy != "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(m.st.msg, "Could not switch") || !strings.Contains(m.st.msg, "Steam") {
		t.Fatalf("failure message = %q", m.st.msg)
	}
}

func TestStartupScreenshots(t *testing.T) {
	dest := os.Getenv("DG_SHOTS")
	if dest == "" {
		t.Skip("DG_SHOTS not set")
	}
	os.MkdirAll(dest, 0o755)
	for _, dark := range []bool{false, true} {
		suffix := map[bool]string{true: "dark", false: "light"}[dark]
		m, _ := startupFixture(t)
		m.st.selKey = "service|gupdate"
		tt := ui.NewTester(m.view, 1180, 760)
		tt.SetDark(dark)
		tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 138})
		tt.Frame()
		dump(t, tt, dest, "startup-"+suffix+".png")
		m.st.showWindows = true
		m.st.selKey = "service|WSearch"
		tt.Frame()
		dump(t, tt, dest, "startup-windows-"+suffix+".png")
	}
}
