// Package appdir locates the folder DumpingGround keeps its state in:
// logs, undo records, uninstall history and pending reviews.
//
// A ".data" folder next to the executable makes the install portable —
// move the folder and everything the app remembers moves with it. When
// that folder cannot be created or written (read-only media, a
// protected location), the per-user fallback is
// %LOCALAPPDATA%\DumpingGround.
package appdir

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

var (
	mu      sync.Mutex
	dir     string
	testDir string // set by tests to pin the location
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
func SetDir(d string) {
	mu.Lock()
	defer mu.Unlock()
	testDir, dir = d, ""
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
