package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dumpingground/dumpingground/internal/appdir"
	"github.com/dumpingground/dumpingground/internal/applog"
	"github.com/dumpingground/dumpingground/internal/hwmon"
)

// energyLedger accumulates watt-hours drawn from the battery, bucketed
// per calendar day. It integrates the driver's own discharge-rate
// telemetry; packs that report no rate fall back to the difference in
// remaining capacity between samples. Only time on battery counts —
// a plugged-in machine draws from the wall, not the pack, and the user
// asked for battery consumption.
type energyLedger struct {
	mu      sync.Mutex
	days    map[string]float64 // "2006-01-02" → Wh discharged that day
	last    time.Time          // last sample instant — dt between ticks
	lastCap int                // last remaining capacity mWh, for the no-rate fallback
	unsaved float64            // Wh accumulated since the last write
	savedAt time.Time
}

func energyPath() string { return filepath.Join(appdir.Dir(), "energy.json") }

// loadEnergy restores the persisted day totals; a missing or corrupt
// file starts the ledger fresh — energy history is informational, never
// worth an error dialog.
func loadEnergy() *energyLedger {
	e := &energyLedger{days: map[string]float64{}, savedAt: time.Now()}
	if data, err := os.ReadFile(energyPath()); err == nil {
		_ = json.Unmarshal(data, &e.days)
	}
	return e
}

// add folds one monitor tick into the ledger. dt is clamped so a paused
// sampler or a sleep/wake gap can't dump hours of phantom draw into one
// bucket.
func (e *energyLedger) add(b *hwmon.BatterySample, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	dt := now.Sub(e.last)
	e.last = now
	if dt < 0 || dt > 10*time.Minute {
		dt = 0
	}
	if b == nil {
		e.lastCap = 0
		return
	}
	var wh float64
	if !b.OnAC && !b.Charging {
		switch {
		case b.RateMW != 0:
			w := float64(b.RateMW)
			if w < 0 {
				w = -w
			}
			wh = w / 1000 * dt.Hours()
		case b.CapMWH > 0 && e.lastCap > 0 && e.lastCap > b.CapMWH:
			// No rate telemetry: the capacity the pack lost between
			// samples is the energy it spent.
			wh = float64(e.lastCap-b.CapMWH) / 1000
		}
	}
	e.lastCap = b.CapMWH
	if wh <= 0 {
		return
	}
	e.days[now.Format("2006-01-02")] += wh
	e.unsaved += wh
	if e.unsaved >= 0.5 || time.Since(e.savedAt) > 60*time.Second {
		e.saveLocked()
	}
}

// flush persists whatever accumulated since the last write — called on
// shutdown so the tail of a session isn't lost.
func (e *energyLedger) flush() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.saveLocked()
}

func (e *energyLedger) saveLocked() {
	data, err := json.Marshal(e.days)
	if err != nil {
		return
	}
	if err := os.WriteFile(energyPath(), data, 0o644); err != nil {
		applog.Logf("save energy ledger: %v", err)
		return
	}
	e.unsaved, e.savedAt = 0, time.Now()
}

// energyTotals carries the four reporting dimensions the battery page
// shows, in watt-hours.
type energyTotals struct {
	Day, Week, Month, Year float64
}

// totals aggregates the ledger: today, the rolling last 7 days, the
// current calendar month and the current calendar year.
func (e *energyLedger) totals(now time.Time) energyTotals {
	var t energyTotals
	today := now.Format("2006-01-02")
	weekFrom := now.AddDate(0, 0, -6)
	y, mo, _ := now.Date()
	monthFrom := time.Date(y, mo, 1, 0, 0, 0, 0, now.Location())
	yearFrom := time.Date(y, 1, 1, 0, 0, 0, 0, now.Location())
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, wh := range e.days {
		d, err := time.ParseInLocation("2006-01-02", k, now.Location())
		if err != nil {
			continue
		}
		if k == today {
			t.Day += wh
		}
		if !d.Before(weekFrom) {
			t.Week += wh
		}
		if !d.Before(monthFrom) {
			t.Month += wh
		}
		if !d.Before(yearFrom) {
			t.Year += wh
		}
	}
	return t
}
