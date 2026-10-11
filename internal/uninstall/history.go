package uninstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// uninstalledRecord remembers one app whose uninstaller removed its
// registry entry. After that, the name is the only handle a leftover
// scan still has — so it is kept indefinitely (the file stays tiny).
type uninstalledRecord struct {
	Name string    `json:"name"`
	When time.Time `json:"when"`
}

func historyPath() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "DumpingGround", "uninstalled.json")
}

func readHistory() []uninstalledRecord {
	data, err := os.ReadFile(historyPath())
	if err != nil {
		return nil
	}
	var recs []uninstalledRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil
	}
	out := recs[:0]
	for _, r := range recs {
		if r.Name != "" {
			out = append(out, r)
		}
	}
	return out
}

func writeHistory(recs []uninstalledRecord) {
	if len(recs) == 0 {
		_ = os.Remove(historyPath())
		return
	}
	data, err := json.Marshal(recs)
	if err != nil {
		return
	}
	p := historyPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o644)
}

// RecordUninstalled notes an app whose registry entry is gone. Called
// once the vendor uninstaller finishes and the key has vanished.
func RecordUninstalled(name string) {
	if name == "" {
		return
	}
	recs := readHistory()
	for i := range recs {
		if recs[i].Name == name {
			recs[i].When = time.Now()
			writeHistory(recs)
			return
		}
	}
	writeHistory(append(recs, uninstalledRecord{Name: name, When: time.Now()}))
}

// HistoryNames lists every app known to have been uninstalled —
// recorded removals plus names recovered from past run logs, which
// predate the history file. Order: oldest record first.
func HistoryNames() []string {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if n = strings.TrimSpace(n); n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, r := range readHistory() {
		add(r.Name)
	}
	for _, n := range logUninstallNames() {
		add(n)
	}
	return names
}

// logUninstallNames recovers app names from past run logs — the
// "Uninstall — X" section headers and "uninstaller exited" lines the
// flow already wrote before this history file existed.
func logUninstallNames() []string {
	logs := filepath.Join(filepath.Dir(historyPath()), "logs")
	ents, err := os.ReadDir(logs)
	if err != nil {
		return nil
	}
	var names []string
	for _, ent := range ents {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(logs, ent.Name()))
		if err != nil || len(data) > 8<<20 {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			switch {
			case strings.HasPrefix(line, "== Uninstall — "):
				names = append(names, strings.TrimSuffix(
					strings.TrimPrefix(line, "== Uninstall — "), " =="))
			default:
				if i := strings.Index(line, "uninstaller exited"); i >= 0 {
					names = append(names, line[i+len("uninstaller exited"):])
				}
			}
		}
	}
	return names
}
