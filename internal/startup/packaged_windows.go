//go:build windows

package startup

import (
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	appxPackagesKey = `Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppModel\Repository\Packages`
	appxStateKey    = `Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppModel\SystemAppData`
)

const (
	startupTaskDisabled         = 0
	startupTaskDisabledByUser   = 1
	startupTaskEnabled          = 2
	startupTaskDisabledByPolicy = 3
	startupTaskEnabledByPolicy  = 4
)

// A packaged app's StartupTask is the switch Windows Settings → Apps →
// Startup shows. Its state lives under the package family name, not the
// versioned package name, so it survives the app's own updates.
type packagedApp struct {
	fullName string
	family   string
	root     string
	display  string
}

type packagedTask struct {
	pkg             packagedApp
	display         string
	executable      string
	taskID          string
	manifestEnabled bool
}

type appxManifest struct {
	Applications []struct {
		Executable     string `xml:"Executable,attr"`
		VisualElements struct {
			DisplayName string `xml:"DisplayName,attr"`
		} `xml:"VisualElements"`
		Extensions struct {
			Extension []struct {
				Category   string `xml:"Category,attr"`
				Executable string `xml:"Executable,attr"`
				Startup    struct {
					TaskID  string `xml:"TaskId,attr"`
					Enabled string `xml:"Enabled,attr"`
					Display string `xml:"DisplayName,attr"`
				} `xml:"StartupTask"`
			} `xml:"Extension"`
		} `xml:"Extensions"`
	} `xml:"Applications>Application"`
}

// readSubkeys lists one registry key's immediate children.
func readSubkeys(root syscall.Handle, path string) []string {
	h, err := openKey(root, path, syscall.KEY_ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer syscall.RegCloseKey(h)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var out []string
	for i := uint32(0); ; i++ {
		name := make([]uint16, 256)
		n := uint32(len(name))
		err := syscall.RegEnumKeyEx(h, i, &name[0], &n, nil, nil, nil, nil)
		if err != nil {
			return out
		}
		out = append(out, syscall.UTF16ToString(name[:n]))
	}
}

// packageFamilyName turns a package full name into the package family
// name Windows uses for per-user package state: name + "_" + publisher.
func packageFamilyName(full string) string {
	first := strings.IndexByte(full, '_')
	last := strings.LastIndexByte(full, '_')
	if first <= 0 || last <= first {
		return ""
	}
	return full[:first] + "_" + full[last+1:]
}

// packagedApps reads the per-user AppX repository. Packages that are only
// staged, not registered for this user, are absent here.
func packagedApps() []packagedApp {
	var out []packagedApp
	for _, full := range readSubkeys(syscall.HKEY_CURRENT_USER, appxPackagesKey) {
		h, err := openKey(syscall.HKEY_CURRENT_USER, appxPackagesKey+`\`+full, syscall.KEY_QUERY_VALUE)
		if err != nil {
			continue
		}
		root := strings.TrimSpace(utf16z(queryValue(h, "PackageRootFolder")))
		display := strings.TrimSpace(utf16z(queryValue(h, "DisplayName")))
		syscall.RegCloseKey(h)
		family := packageFamilyName(full)
		if root == "" || family == "" {
			continue
		}
		out = append(out, packagedApp{fullName: full, family: family, root: root, display: display})
	}
	return out
}

func friendlyPackageName(family string) string {
	name, _, _ := strings.Cut(family, "_")
	return strings.ReplaceAll(name, ".", " ")
}

func parsePackagedTasks(pkg packagedApp, manifest []byte) []packagedTask {
	var m appxManifest
	if err := xml.Unmarshal(manifest, &m); err != nil {
		return nil
	}
	var out []packagedTask
	for _, app := range m.Applications {
		for _, ext := range app.Extensions.Extension {
			if ext.Category != "windows.startupTask" || ext.Startup.TaskID == "" {
				continue
			}
			exe := ext.Executable
			if exe == "" {
				exe = app.Executable
			}
			display := ext.Startup.Display
			if display == "" || strings.HasPrefix(display, "ms-resource:") {
				display = app.VisualElements.DisplayName
			}
			if display == "" || strings.HasPrefix(display, "ms-resource:") {
				display = pkg.display
			}
			if display == "" || strings.HasPrefix(display, "ms-resource:") {
				display = friendlyPackageName(pkg.family)
			}
			out = append(out, packagedTask{
				pkg:             pkg,
				display:         display,
				executable:      exe,
				taskID:          ext.Startup.TaskID,
				manifestEnabled: strings.EqualFold(ext.Startup.Enabled, "true"),
			})
		}
	}
	return out
}

func packagedTaskStatePath(family, taskID string) string {
	return appxStateKey + `\` + family + `\` + taskID
}

func packagedTaskState(path string) (uint32, bool) {
	h, err := openKey(syscall.HKEY_CURRENT_USER, path, syscall.KEY_QUERY_VALUE)
	if err != nil {
		return 0, false
	}
	defer syscall.RegCloseKey(h)
	b := queryValue(h, "State")
	if len(b) < 4 {
		return 0, false
	}
	return le32(b), true
}

// packagedTaskItem turns one manifest declaration into a row. A missing
// State is off: an enabled manifest entry still becomes active only after
// the app or the user enables startup once.
func packagedTaskItem(task packagedTask, running map[string]bool) Item {
	path := packagedTaskStatePath(task.pkg.family, task.taskID)
	state, hasState := packagedTaskState(path)
	enabled := state == startupTaskEnabled || state == startupTaskEnabledByPolicy
	if !hasState {
		enabled = false
	}
	exe := ""
	if task.executable != "" {
		exe = filepath.Join(task.pkg.root, filepath.FromSlash(task.executable))
	}
	it := Item{
		Kind: KindPackaged, Key: "packaged|" + task.pkg.family + "|" + task.taskID,
		Name: task.display, Command: exe, Exe: exe,
		Location: "Packaged app " + task.pkg.family, reg: `HKCU\` + path,
		Scope: "Current user", Trigger: "At sign-in", Enabled: enabled,
		Running: exe != "" && running[strings.ToLower(exe)],
		Package: task.pkg.family, value: path,
	}
	if hasState && (state == startupTaskDisabledByPolicy || state == startupTaskEnabledByPolicy) {
		it.Locked = "Managed by administrator policy"
	}
	return it
}

// listPackagedTasks finds StartupTask declarations in the manifests of the
// packages registered for this user. Unlike a packaged service, this is
// the user's own startup switch and Windows is expected to preserve it
// across package updates because the state key is version-independent.
func listPackagedTasks(running map[string]bool) []Item {
	return packagedTaskItems(packagedApps(), running)
}

func packagedTaskItems(pkgs []packagedApp, running map[string]bool) []Item {
	var items []Item
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		b, err := os.ReadFile(filepath.Join(pkg.root, "AppxManifest.xml"))
		if err != nil {
			continue
		}
		for _, task := range parsePackagedTasks(pkg, b) {
			key := strings.ToLower(task.pkg.family + `\` + task.taskID)
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, packagedTaskItem(task, running))
		}
	}
	return items
}

// setPackagedTaskEnabled writes the same per-user state used by Windows
// Settings' Startup page. The manifest, package registration and app data
// are untouched; enable records that the user enabled startup once.
func setPackagedTaskEnabled(it Item, on bool) error {
	path := it.value
	if !strings.HasPrefix(strings.ToUpper(path), strings.ToUpper(appxStateKey)+`\`) {
		return fmt.Errorf("%q is not a packaged startup task", it.Name)
	}
	if state, ok := packagedTaskState(path); ok && (state == startupTaskDisabledByPolicy || state == startupTaskEnabledByPolicy) {
		return fmt.Errorf("%q is managed by administrator policy", it.Name)
	}
	h, err := createKey(syscall.HKEY_CURRENT_USER, path)
	if err != nil {
		return err
	}
	defer syscall.RegCloseKey(h)
	var b [4]byte
	if on {
		binary.LittleEndian.PutUint32(b[:], 1)
		if err := regSet(h, "UserEnabledStartupOnce", syscall.REG_DWORD, b[:]); err != nil {
			return err
		}
	} else {
		var stamp [8]byte
		binary.LittleEndian.PutUint64(stamp[:], uint64(time.Now().Unix()))
		if err := regSet(h, "LastDisabledTime", syscall.REG_QWORD, stamp[:]); err != nil {
			return err
		}
	}
	state := uint32(startupTaskDisabledByUser)
	if on {
		state = startupTaskEnabled
	}
	binary.LittleEndian.PutUint32(b[:], state)
	return regSet(h, "State", syscall.REG_DWORD, b[:])
}
