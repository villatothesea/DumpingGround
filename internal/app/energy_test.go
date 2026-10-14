package app

import (
	"math"
	"testing"
	"time"

	"github.com/dumpingground/dumpingground/internal/appdir"
	"github.com/dumpingground/dumpingground/internal/hwmon"
)

// A pack discharging at 12 W for 30 s has spent 0.1 Wh.
func TestEnergyRateIntegration(t *testing.T) {
	e := &energyLedger{days: map[string]float64{}}
	t0 := time.Now()
	e.add(&hwmon.BatterySample{OnAC: false, RateMW: -12000}, t0)
	e.add(&hwmon.BatterySample{OnAC: false, RateMW: -12000}, t0.Add(30*time.Second))
	got := e.totals(t0.Add(30 * time.Second)).Day
	if math.Abs(got-0.1) > 0.001 {
		t.Fatalf("day total = %v Wh, want ≈0.1", got)
	}
}

// Plugged in is wall power — nothing accrues against the battery.
func TestEnergyNoAccrualOnAC(t *testing.T) {
	e := &energyLedger{days: map[string]float64{}}
	t0 := time.Now()
	e.add(&hwmon.BatterySample{OnAC: true, Charging: true, RateMW: 15000}, t0)
	e.add(&hwmon.BatterySample{OnAC: true, Charging: true, RateMW: 15000}, t0.Add(time.Minute))
	if got := e.totals(t0).Day; got != 0 {
		t.Fatalf("on-AC accrual = %v, want 0", got)
	}
}

// Packs that never report a rate still accrue through the capacity the
// driver says they lost between samples.
func TestEnergyCapacityFallback(t *testing.T) {
	e := &energyLedger{days: map[string]float64{}}
	t0 := time.Now()
	e.add(&hwmon.BatterySample{RateMW: 0, CapMWH: 40000}, t0)
	e.add(&hwmon.BatterySample{RateMW: 0, CapMWH: 39800}, t0.Add(time.Minute))
	if got := e.totals(t0).Day; math.Abs(got-0.2) > 0.001 {
		t.Fatalf("fallback total = %v Wh, want ≈0.2", got)
	}
}

// A capacity reading that jumps up is a charge, not negative draw.
func TestEnergyCapacityJumpUpIgnored(t *testing.T) {
	e := &energyLedger{days: map[string]float64{}}
	t0 := time.Now()
	e.add(&hwmon.BatterySample{CapMWH: 20000}, t0)
	e.add(&hwmon.BatterySample{CapMWH: 40000}, t0.Add(time.Minute))
	if got := e.totals(t0).Day; got != 0 {
		t.Fatalf("charge counted as draw: %v", got)
	}
}

// Week/month/year roll up their calendar windows; a day outside each
// window stays out of it.
func TestEnergyTotalsWindows(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	e := &energyLedger{days: map[string]float64{
		now.Format("2006-01-02"):                                             10, // today
		now.AddDate(0, 0, -3).Format("2006-01-02"):                           5,  // inside the week
		now.AddDate(0, 0, -10).Format("2006-01-02"):                          7,  // June, outside the week
		time.Date(2026, 1, 2, 0, 0, 0, 0, time.Local).Format("2006-01-02"):   20, // this year, last month
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.Local).Format("2006-01-02"): 50, // last year
	}}
	got := e.totals(now)
	if got.Day != 10 || got.Week != 15 || got.Month != 22 || got.Year != 42 {
		t.Fatalf("totals = %+v", got)
	}
}

// Persisted days come back through loadEnergy — the ledger survives
// restarts because history is the whole point.
func TestEnergyPersistence(t *testing.T) {
	appdir.SetDir(t.TempDir())
	defer appdir.SetDir("")
	e := loadEnergy()
	t0 := time.Now()
	e.add(&hwmon.BatterySample{RateMW: -3600000}, t0) // 3.6 kW for 1 s = 1 Wh
	e.add(&hwmon.BatterySample{RateMW: -3600000}, t0.Add(time.Second))
	e.flush()
	e2 := loadEnergy()
	got := e2.totals(t0).Day
	if math.Abs(got-1.0) > 0.01 {
		t.Fatalf("reloaded day = %v Wh, want ≈1", got)
	}
}
