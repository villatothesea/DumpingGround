// Package applog keeps a dated, append-only record of what
// DumpingGround changed on the machine. Every destructive action —
// junk deletion, leftover removal, registry edits — writes lines here
// so the operation can be reviewed or reconstructed afterwards.
//
// Logs live under %LOCALAPPDATA%\DumpingGround\logs and are pruned to
// a fixed retention window; logging itself never fails an operation.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// retainDays is the retention window — files older than this are
// deleted by Prune, which runs once at startup.
const retainDays = 30

var (
	mu     sync.Mutex
	dir    string
	testDir string // set by tests to redirect output
)

// Dir returns the log directory, creating it on first use. Tests can
// redirect it with SetDir.
func Dir() string {
	if testDir != "" {
		return testDir
	}
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		dir = filepath.Join(base, "DumpingGround", "logs")
		_ = os.MkdirAll(dir, 0o755)
	}
	return dir
}

// SetDir redirects the log directory; passing "" restores the default.
// Intended for tests.
func SetDir(d string) {
	mu.Lock()
	defer mu.Unlock()
	testDir = d
	dir = ""
}

func path() string {
	return filepath.Join(Dir(), "dumpingground-"+time.Now().Format("2006-01-02")+".log")
}

// Section writes a timestamped section header marking the start of an
// operation, e.g. Section("Uninstall sweep — Obsidian").
func Section(title string) {
	write("\n== " + title + " ==")
}

// Logf appends one timestamped line.
func Logf(format string, args ...any) {
	write(fmt.Sprintf(format, args...))
}

func write(line string) {
	mu.Lock()
	defer mu.Unlock()
	f, err := os.OpenFile(path(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05.000"), line)
}

// Prune deletes log files older than the retention window. Called once
// at startup; safe to call again.
func Prune() {
	cutoff := time.Now().AddDate(0, 0, -retainDays)
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "dumpingground-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			p := filepath.Join(Dir(), e.Name())
			_ = os.Remove(p)
		}
	}
}

// Read returns the most recent log file's contents.
func Read() string {
	data, _ := os.ReadFile(path())
	return string(data)
}
