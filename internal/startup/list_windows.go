//go:build windows

package startup

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/dumpingground/dumpingground/internal/applog"
)

// List reads every source and returns the items unsorted. It reads
// the registry, the Task Scheduler store and the files' version
// resources — a few hundred milliseconds, so call it off the UI thread.
func List() []Item {
	running := runningImages()
	items := listApps(running)
	items = append(items, listServices()...)
	items = append(items, listTasks(running)...)
	enrich(items)
	return items
}

// windowsLock is shown on components of Windows: they are listed so
// nothing is hidden, but switching them is left to Windows.
const windowsLock = "Part of Windows — managed by Windows, shown read-only"

// enrich fills in publisher and description from each program's version
// resource, decides what counts as a Windows component and locks those.
// Lookups run in parallel — each reads a file — and are cached, so the
// hundred services sharing svchost.exe cost one read.
//
// "Windows" is decided by who made the program, not by where it sits: OEM
// driver services live under System32\DriverStore yet are the vendor's.
// Tasks under \Microsoft\Windows\ are Windows by definition and skip the
// lookup — there are hundreds of them.
func enrich(items []Item) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range items {
		it := &items[i]
		if it.Kind == KindTask && it.Windows {
			it.Publisher = "Microsoft Windows"
			continue
		}
		if it.Exe == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			vi := fileInfo(it.Exe)
			if vi.Company != "" {
				it.Publisher = vi.Company
			} else if p := packagePublisher(it.Exe); p != "" && it.Publisher == "" {
				it.Publisher = p
			}
			if vi.Desc != "" {
				it.Desc = vi.Desc
			}
			if it.Kind == KindService {
				it.Windows = isWindowsService(it.Exe, vi)
			}
		}()
	}
	wg.Wait()
	for i := range items {
		it := &items[i]
		if it.Windows && (it.Kind == KindService || it.Kind == KindTask) {
			it.Locked = windowsLock
			if it.Publisher == "" {
				it.Publisher = "Microsoft Windows"
			}
		}
	}
}

// isWindowsService decides whether a service is a Windows component:
// Microsoft's own program for Windows (svchost, lsass, Defender, WSL), or,
// where the file says nothing, a program in the Windows directory outside
// the folders vendors install drivers into.
func isWindowsService(exe string, vi verInfo) bool {
	if strings.Contains(vi.Company, "Microsoft") {
		return strings.Contains(vi.Product, "Windows") || underWindowsDir(exe)
	}
	if vi.Company != "" {
		return false
	}
	low := strings.ToLower(exe)
	return underWindowsDir(exe) && !strings.Contains(low, `\driverstore\`) && !strings.Contains(low, `\system32\drivers\`)
}

// packagePublisher names the maker of a packaged app from its folder —
// "...\WindowsApps\OpenAI.Codex_1.0_x64__abc\..." gives "OpenAI" — for
// programs whose version resource cannot be read.
func packagePublisher(exe string) string {
	const marker = `\windowsapps\`
	i := strings.Index(strings.ToLower(exe), marker)
	if i < 0 {
		return ""
	}
	pkg, _, _ := strings.Cut(exe[i+len(marker):], `\`)
	pub, _, ok := strings.Cut(pkg, ".")
	if !ok {
		return ""
	}
	return pub
}

// SetEnabled switches an item on or off and logs the change. Nothing is
// deleted: see the package comment for how each kind is made reversible.
func SetEnabled(it Item, on bool) error {
	if it.Locked != "" {
		return errors.New(it.Locked)
	}
	var err error
	switch it.Kind {
	case KindRun, KindFolder:
		err = setAppEnabled(it, on)
	case KindService:
		err = setServiceEnabled(it, on)
	case KindTask:
		err = setTaskEnabled(it, on)
	default:
		err = fmt.Errorf("unknown startup kind %d", it.Kind)
	}
	verb := map[bool]string{true: "enabled", false: "disabled"}[on]
	if err != nil {
		applog.Logf("startup: could not turn %s %s %q — %v", map[bool]string{true: "on", false: "off"}[on], strings.ToLower(it.Kind.String()), it.Name, err)
		return err
	}
	applog.Logf("startup: %s %s %q — %s — %s", verb, strings.ToLower(it.Kind.String()), it.Name, it.Location, it.Command)
	return nil
}

// OpenInRegedit opens Registry Editor on the item's key.
func OpenInRegedit(it Item) error {
	if it.reg == "" {
		return errors.New("this item has no registry key")
	}
	h, err := createKey(syscall.HKEY_CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Applets\Regedit`)
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(h)
	full := strings.Replace(strings.Replace(it.reg, "HKLM", "HKEY_LOCAL_MACHINE", 1), "HKCU", "HKEY_CURRENT_USER", 1)
	data, _ := syscall.UTF16FromString(full)
	b := make([]byte, len(data)*2)
	for i, c := range data {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	if err := regSet(h, "LastKey", syscall.REG_SZ, b); err != nil {
		return err
	}
	return exec.Command("regedit.exe").Start()
}
