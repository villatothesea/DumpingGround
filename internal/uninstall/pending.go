package uninstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/dumpingground/dumpingground/internal/appdir"
)

// pendingReview is one app's unfinished leftover review: the app
// closed between the uninstaller exiting and the user acting on the
// leftover list. Reviews persist so the next launch reopens them
// rather than dropping the work silently — one per app, upserted by
// name so a second sweep of a different app never eats the first.
type pendingReview struct {
	Name  string    `json:"name"`
	When  time.Time `json:"when"`
	Items []Item    `json:"items"`
}

func pendingPath() string {
	return filepath.Join(appdir.Dir(), "pending-sweep.json")
}

func readPending() []pendingReview {
	data, err := os.ReadFile(pendingPath())
	if err != nil {
		return nil
	}
	var reviews []pendingReview
	if err := json.Unmarshal(data, &reviews); err != nil {
		return nil
	}
	// A review older than a week describes state too stale to trust.
	var live []pendingReview
	for _, r := range reviews {
		if r.Name != "" && len(r.Items) > 0 && time.Since(r.When) <= 7*24*time.Hour {
			live = append(live, r)
		}
	}
	return live
}

func writePending(reviews []pendingReview) {
	if len(reviews) == 0 {
		_ = os.Remove(pendingPath())
		return
	}
	data, err := json.Marshal(reviews)
	if err != nil {
		return
	}
	p := pendingPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o644)
}

// SavePending records (or replaces) one app's leftover review. An
// empty item list clears that app's review instead.
func SavePending(name string, items []Item) {
	reviews := readPending()
	if len(items) == 0 {
		writePending(dropPending(reviews, name))
		return
	}
	for i := range reviews {
		if reviews[i].Name == name {
			reviews[i].When, reviews[i].Items = time.Now(), items
			writePending(reviews)
			return
		}
	}
	writePending(append(reviews, pendingReview{Name: name, When: time.Now(), Items: items}))
}

// LoadPending returns the oldest leftover review an earlier run left
// behind, or nil when none was saved.
func LoadPending() (string, []Item) {
	reviews := readPending()
	if len(reviews) == 0 {
		return "", nil
	}
	return reviews[0].Name, reviews[0].Items
}

// ClearPending drops one app's saved review — the user finished it or
// chose to keep the leftovers.
func ClearPending(name string) {
	writePending(dropPending(readPending(), name))
}

func dropPending(reviews []pendingReview, name string) []pendingReview {
	out := reviews[:0]
	for _, r := range reviews {
		if r.Name != name {
			out = append(out, r)
		}
	}
	return out
}
