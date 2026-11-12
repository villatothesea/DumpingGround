// Package appdir locates the folder DumpingGround keeps its state in:
// logs, undo records, uninstall history and pending reviews.
//
// A ".data" folder next to the executable makes the install portable —
// move the folder and everything the app remembers moves with it. When
// that folder cannot be created or written (read-only media, a
// protected location), the per-user fallback is
// %LOCALAPPDATA%\DumpingGround.
//
// Preferences are the exception: they also have a canonical copy in the
// per-user folder so build/, release/ and moved launchers see the same
// settings. Portable folders receive a mirrored settings.json, keeping
// .data complete enough to move to another machine.
package appdir

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var (
	mu            sync.Mutex
	dir           string
	testDir       string // set by tests to pin the location
	testSharedDir string // set by tests to pin the per-user location
)

// Dir returns the state directory. The portable ".data" folder beside
// the executable wins whenever it is writable; the result is cached.
func Dir() string {
	mu.Lock()
	defer mu.Unlock()
	if testDir != "" {
		return testDir
	}
	if dir == "" {
		exe, _ := os.Executable()
		dir = resolve(exe, legacyDir())
	}
	return dir
}

// SetDir pins the state directory for tests; "" restores resolution.
// The per-user directory follows it so tests never touch real user state.
func SetDir(d string) { SetDirs(d, d) }

// SetDirs pins the portable and per-user locations separately for tests.
func SetDirs(data, shared string) {
	mu.Lock()
	defer mu.Unlock()
	testDir, testSharedDir, dir = data, shared, ""
}

// SharedDir returns the stable per-user directory. Preferences use it so
// copies launched from different paths share one settings file, while the
// portable ".data" folders stay full-state snapshots and migration sources.
func SharedDir() string {
	mu.Lock()
	defer mu.Unlock()
	if testSharedDir != "" {
		return testSharedDir
	}
	if testDir != "" {
		return testDir
	}
	return legacyDir()
}

// legacyDir is where versions before the portable folder kept state.
func legacyDir() string {
	base, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "DumpingGround")
}

func resolve(exe, legacy string) string {
	if exe != "" {
		portable := filepath.Join(filepath.Dir(exe), ".data")
		_, statErr := os.Stat(portable)
		if err := os.MkdirAll(portable, 0o755); err == nil && writable(portable) {
			if statErr != nil {
				migrate(legacy, portable)
			}
			return portable
		}
	}
	return legacy
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// DataDirs returns the current state folder, folders this user has run
// from before, and nearby DumpingGround copies' ".data" folders. Settings
// merge across all of them; other state stays per-location and portable.
func DataDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		if seen[d] || strings.EqualFold(d, SharedDir()) {
			return
		}
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	add(Dir())
	if exe, err := os.Executable(); err == nil {
		for _, d := range nearbyDataDirs(exe) {
			add(d)
		}
	}
	for _, d := range registryDirs() {
		add(d)
	}
	return out
}

// RememberDir records a state folder in the per-user registry. A copy at
// a path never seen before can then find settings left by earlier copies.
func RememberDir(d string) {
	if d == "" {
		return
	}
	d = filepath.Clean(d)
	shared := SharedDir()
	if strings.EqualFold(d, shared) {
		return
	}
	dirs := registryDirs()
	if slices.IndexFunc(dirs, func(old string) bool { return strings.EqualFold(old, d) }) < 0 {
		dirs = append([]string{d}, dirs...)
		if len(dirs) > 64 {
			dirs = dirs[:64]
		}
		if err := os.MkdirAll(shared, 0o755); err != nil {
			return
		}
		data, err := json.Marshal(dirs)
		if err == nil {
			_ = os.WriteFile(registryPath(), data, 0o644)
		}
	}
}

func registryPath() string { return filepath.Join(SharedDir(), "data-dirs.json") }

func registryDirs() []string {
	data, err := os.ReadFile(registryPath())
	if err != nil {
		return nil
	}
	var dirs []string
	if json.Unmarshal(data, &dirs) != nil {
		return nil
	}
	return dirs
}

// nearbyDataDirs checks only directories that contain a DumpingGround
// executable: the repo's build/release/root copies, or copies grouped in
// one portable folder. It deliberately does not scan arbitrary .data
// directories, which can belong to other apps.
func nearbyDataDirs(exe string) []string {
	if exe == "" {
		return nil
	}
	parent := filepath.Dir(filepath.Dir(exe))
	var out []string
	add := func(dir string) {
		if !hasAppExe(dir) {
			return
		}
		if st, err := os.Stat(filepath.Join(dir, ".data")); err == nil && st.IsDir() {
			out = append(out, filepath.Join(dir, ".data"))
		}
	}
	add(parent)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return out
	}
	for i, e := range entries {
		if i >= 256 {
			break
		}
		if e.IsDir() {
			add(filepath.Join(parent, e.Name()))
		}
	}
	return out
}

func hasAppExe(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if !e.IsDir() && strings.HasSuffix(name, ".exe") && strings.Contains(name, "dumpingground") {
			return true
		}
	}
	return false
}

// migrate copies state an earlier run left in the per-user folder into
// a freshly created portable one, so history, undo records and logs
// travel with the exe instead of being silently abandoned. Files
// already present are not overwritten.
func migrate(src, dst string) {
	if src == dst {
		return
	}
	_ = filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return nil
		}
		to := filepath.Join(dst, rel)
		if e.IsDir() {
			_ = os.MkdirAll(to, 0o755)
			return nil
		}
		if _, err := os.Stat(to); err == nil {
			return nil // never overwrite portable state
		}
		from, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer from.Close()
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return nil
		}
		out, err := os.Create(to)
		if err != nil {
			return nil
		}
		if _, err := io.Copy(out, from); err == nil {
			_ = out.Close()
		} else {
			_ = out.Close()
			_ = os.Remove(to)
		}
		return nil
	})
}
