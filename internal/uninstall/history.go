package uninstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// uninstalledRecord remembers one app whose uninstaller ran. The name
// alone is a thin handle, so the entry's location, publisher and key
// go with it — enough for the scan to re-identify its leftovers and
// to tell a live same-named app (a portable copy writing after When)
// from a truly abandoned folder.
type uninstalledRecord struct {
	Name      string    `json:"name"`
	Location  string    `json:"location,omitempty"`
	Publisher string    `json:"publisher,omitempty"`
	Key       string    `json:"key,omitempty"`
	When      time.Time `json:"when"`
	// Gone marks the record whose registry entry was seen removed;
	// false records were written at launch and may describe an
	// uninstall that never finished.
	Gone bool `json:"gone,omitempty"`
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

// recordEntry upserts the app's record — the uninstaller may never
// finish within our watch window or the app may close mid-watch, so
// the record exists from the moment the uninstaller launches and only
// the When/Gone fields move afterwards.
func recordEntry(e *Entry, gone bool) {
	if e == nil || e.Name == "" {
		return
	}
	recs := readHistory()
	for i := range recs {
		if recs[i].Name == e.Name {
			recs[i].Location, recs[i].Publisher, recs[i].Key = e.Location, e.Publisher, e.Key
			recs[i].When, recs[i].Gone = time.Now(), gone
			writeHistory(recs)
			return
		}
	}
	writeHistory(append(recs, uninstalledRecord{
		Name: e.Name, Location: e.Location, Publisher: e.Publisher,
		Key: e.Key, When: time.Now(), Gone: gone,
	}))
}

// RecordLaunch notes that the app's uninstaller was started — before
// its outcome is known, so a crash or a late finish still leaves a
// name the next scan can look for.
func RecordLaunch(e *Entry) { recordEntry(e, false) }

// RecordUninstalled notes an app whose registry entry is gone. Called
// once the vendor uninstaller finishes and the key has vanished.
func RecordUninstalled(e *Entry) { recordEntry(e, true) }

// History lists every app known to have been uninstalled — recorded
// removals plus bare names recovered from past run logs, which predate
// the history file. Order: oldest record first.
func History() []uninstalledRecord {
	seen := map[string]bool{}
	var out []uninstalledRecord
	add := func(r uninstalledRecord) {
		if r.Name = strings.TrimSpace(r.Name); r.Name != "" && !seen[r.Name] {
			seen[r.Name] = true
			out = append(out, r)
		}
	}
	for _, r := range readHistory() {
		add(r)
	}
	for _, n := range logUninstallNames() {
		add(uninstalledRecord{Name: n})
	}
	return out
}

// HistoryNames lists the names in History, oldest first.
func HistoryNames() []string {
	var names []string
	for _, r := range History() {
		names = append(names, r.Name)
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
