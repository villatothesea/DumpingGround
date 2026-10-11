package app

import (
	"testing"

	"github.com/dumpingground/dumpingground/internal/disk"
)

// Retargeting the scan — picking another drive, browsing elsewhere, or
// hitting Scan — must leave every overlay mode behind, or the junk/rust
// page early-return hides the result the user just asked for.
func TestSelectDriveExitsJunkMode(t *testing.T) {
	m := testModel(t)
	m.junkMode = true
	m.junkReport = &disk.JunkReport{}
	// Re-clicking the already-selected drive keeps the page up.
	m.selectDrive("C:\\")
	if !m.junkMode {
		t.Fatal("same-drive click should not exit the junk page")
	}
	m.selectDrive("D:\\")
	if m.junkMode {
		t.Fatal("switching drives must exit the junk page")
	}
}

func TestStartScanExitsJunkMode(t *testing.T) {
	m := testModel(t)
	m.junkMode = true
	m.junkReport = &disk.JunkReport{}
	m.diskPath = "D:\\"
	m.startScan()
	if m.junkMode {
		t.Fatal("a disk scan must exit the junk page")
	}
	m.cancelScan()
}
