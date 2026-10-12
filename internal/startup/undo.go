package startup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// serviceState is what a service looked like before DumpingGround
// disabled it, so Enable can put it back exactly — an automatic service
// may have been delayed-start, and "Enable" must not guess.
type serviceState struct {
	Start   uint32    `json:"start"`
	Delayed bool      `json:"delayed"`
	When    time.Time `json:"when"`
}

var undoMu sync.Mutex

func undoPath() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "DumpingGround", "startup-undo.json")
}

func undoLoad() map[string]serviceState {
	m := map[string]serviceState{}
	if b, err := os.ReadFile(undoPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func undoSave(m map[string]serviceState) error {
	p := undoPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// remember stores the state to restore, keeping the first record when an
// item is disabled twice — the second call would otherwise overwrite the
// real original with "disabled".
func remember(key string, s serviceState) error {
	undoMu.Lock()
	defer undoMu.Unlock()
	m := undoLoad()
	if _, ok := m[key]; ok {
		return nil
	}
	s.When = time.Now()
	m[key] = s
	return undoSave(m)
}

func recall(key string) (serviceState, bool) {
	undoMu.Lock()
	defer undoMu.Unlock()
	s, ok := undoLoad()[key]
	return s, ok
}

func forget(key string) {
	undoMu.Lock()
	defer undoMu.Unlock()
	m := undoLoad()
	if _, ok := m[key]; ok {
		delete(m, key)
		_ = undoSave(m)
	}
}
