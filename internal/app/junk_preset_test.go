package app

import (
	"testing"

	"github.com/dumpingground/dumpingground/internal/disk"
)

// TestJunkPresets pins the preset boundaries: Light covers exactly the
// SAFE band minus the Recycle Bin, Deep reaches the end of MODERATE,
// and no preset ever touches a REVIEW row.
func TestJunkPresets(t *testing.T) {
	light, deep := junkPresets[0], junkPresets[1]

	for c := 0; c <= disk.JunkCatDrvExtract; c++ {
		want := c != disk.JunkCatRecycleBin
		if light.covers(c) != want {
			t.Errorf("Light covers(%d) = %v, want %v", c, light.covers(c), want)
		}
	}
	for _, c := range []int{disk.JunkCatUpdateDl, disk.JunkCatDumps, disk.JunkCatWinLogs, disk.JunkCatDevCaches} {
		if light.covers(c) {
			t.Errorf("Light must not cover MODERATE category %d", c)
		}
		if !deep.covers(c) {
			t.Errorf("Deep must cover MODERATE category %d", c)
		}
	}
	// Review rows are a judgment call — a plan must never bundle them.
	for _, c := range []int{disk.JunkCatWindowsOld, disk.JunkCatFound, disk.JunkCatWinSxS} {
		for i, p := range junkPresets {
			if p.covers(c) {
				t.Errorf("preset %d covers review category %d", i, c)
			}
		}
	}
}
