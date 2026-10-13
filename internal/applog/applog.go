// Package applog keeps a dated, append-only record of what
// DumpingGround changed on the machine. Every destructive action —
// junk deletion, leftover removal, registry edits — writes lines here
// so the operation can be reviewed or reconstructed afterwards.
//
// Logs live under <state dir>\logs — the portable ".data" folder next
// to the exe, or %LOCALAPPDATA%\DumpingGround when that isn't writable —
// and are pruned to a fixed retention window; logging itself never
// fails an operation.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dumpingground/dumpingground/internal/appdir"
)

// retainDays is the retention window — files older than this are
// deleted by Prune, which runs once at startup.
const retainDays = 30

var (
	mu      sync.Mutex
	dir     string
	testDir string // set by tests to redirect output
)

// Dir returns the log directory, creating it on first use. Tests can
// redirect it with SetDir.
func Dir() string {
	if testDir != "" {
		return testDir
	}
	if dir == "" {
		dir = filepath.Join(appdir.Dir(), "logs")
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
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05.000"), escape(line))
}

// escape rewrites control bytes as \n, \r, \t or \xHH before a line is
// appended — a filename or app name carrying a newline must not be
// able to forge extra log entries. Backslashes stay literal so paths
// read exactly as they are on disk.
func escape(s string) string {
	if !strings.ContainsAny(s, "\n\r\t") && strings.IndexFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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

// Recent returns the tail of the log — the last n lines across the
// newest log files, oldest first. Dates come from the filenames; each
// line keeps its own timestamp. Read-only: the view parses nothing.
func Recent(n int) []string {
	if n <= 0 {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(Dir(), "dumpingground-*.log"))
	if err != nil {
		return nil
	}
	// Sort descending by filename — the date is in the name.
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	var lines []string
	for _, f := range matches {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		day := strings.TrimSuffix(filepath.Base(f), ".log")
		day = strings.TrimPrefix(day, "dumpingground-")
		var here []string
		for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if strings.TrimSpace(l) != "" {
				here = append(here, day+"  "+l)
			}
		}
		lines = append(here, lines...) // newest file's lines go last
		if len(lines) >= n {
			break
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
