package uninstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// pending is an unfinished leftover review: the app closed between the
// uninstaller exiting and the user acting on the leftover list. It is
// persisted so the next launch reopens the review rather than dropping
// the work silently.
type pending struct {
	Name  string    `json:"name"`
	When  time.Time `json:"when"`
	Items []Item    `json:"items"`
}

func pendingPath() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "DumpingGround", "pending-sweep.json")
}

// SavePending records a leftover review waiting on the user.
func SavePending(name string, items []Item) {
	if len(items) == 0 {
		ClearPending()
		return
	}
	data, err := json.Marshal(pending{Name: name, When: time.Now(), Items: items})
	if err != nil {
		return
	}
	p := pendingPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o644)
}

// LoadPending returns the leftover review an earlier run left behind,
// or nil when none was saved. Entries over a week old are dropped —
// the leftover state is too stale to trust by then.
func LoadPending() (string, []Item) {
	data, err := os.ReadFile(pendingPath())
	if err != nil {
		return "", nil
	}
	var p pending
	if err := json.Unmarshal(data, &p); err != nil || p.Name == "" || len(p.Items) == 0 {
		return "", nil
	}
	if time.Since(p.When) > 7*24*time.Hour {
		ClearPending()
		return "", nil
	}
	return p.Name, p.Items
}

// ClearPending drops the saved review — the user finished it or chose
// to keep the leftovers.
func ClearPending() {
	_ = os.Remove(pendingPath())
}
