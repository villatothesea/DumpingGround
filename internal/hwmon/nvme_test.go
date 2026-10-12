package hwmon

import (
	"encoding/binary"
	"testing"
)

func TestParseNVMeHealthLog(t *testing.T) {
	b := make([]byte, 512)
	b[0] = 0x04 // "subsystem reliability degraded"
	binary.LittleEndian.PutUint16(b[1:], 273+46)
	b[3], b[4], b[5] = 97, 10, 12
	binary.LittleEndian.PutUint64(b[32:], 1234)  // data units read
	binary.LittleEndian.PutUint64(b[48:], 5678)  // data units written
	binary.LittleEndian.PutUint64(b[128:], 1614) // power-on hours
	binary.LittleEndian.PutUint64(b[144:], 7)    // unsafe shutdowns
	binary.LittleEndian.PutUint64(b[160:], 3)    // media errors
	binary.LittleEndian.PutUint32(b[196:], 5)    // critical temperature minutes

	h, ok := parseNVMeHealthLog(b)
	if !ok {
		t.Fatal("parse failed on a 512-byte log")
	}
	if h.TempC != 46 {
		t.Errorf("TempC = %d, want 46", h.TempC)
	}
	if h.AvailableSpare != 97 || h.AvailableSpareThreshold != 10 || h.PercentageUsed != 12 {
		t.Errorf("spare/used = %d/%d, %d", h.AvailableSpare, h.AvailableSpareThreshold, h.PercentageUsed)
	}
	if h.DataRead != 1234*1000*512 || h.DataWritten != 5678*1000*512 {
		t.Errorf("data read/written = %d/%d", h.DataRead, h.DataWritten)
	}
	if h.PowerOnHours != 1614 || h.UnsafeShutdowns != 7 || h.MediaErrors != 3 {
		t.Errorf("hours/shutdowns/media = %d/%d/%d", h.PowerOnHours, h.UnsafeShutdowns, h.MediaErrors)
	}
	if h.CriticalTempMinutes != 5 {
		t.Errorf("critical temp minutes = %d, want 5", h.CriticalTempMinutes)
	}
	if _, ok := parseNVMeHealthLog(make([]byte, 100)); ok {
		t.Error("a truncated log parsed as valid")
	}
}

func TestJudgeHealth(t *testing.T) {
	good := &DiskHealth{OK: true, AvailableSpare: 100, AvailableSpareThreshold: 5}
	for _, tc := range []struct {
		name   string
		h      *DiskHealth
		status string
	}{
		{"no reading", nil, ""},
		{"unreadable", &DiskHealth{}, ""},
		{"healthy", good, "Good"},
		{"spare below threshold", &DiskHealth{OK: true, AvailableSpare: 5, AvailableSpareThreshold: 10}, "Replace"},
		{"critical warning", &DiskHealth{OK: true, AvailableSpare: 100, AvailableSpareThreshold: 10, CriticalWarning: 0x08}, "Replace"},
		{"endurance high", &DiskHealth{OK: true, AvailableSpare: 100, AvailableSpareThreshold: 10, PercentageUsed: 80}, "Caution"},
		{"media errors", &DiskHealth{OK: true, AvailableSpare: 100, AvailableSpareThreshold: 10, MediaErrors: 1}, "Caution"},
		{"temperature history", &DiskHealth{OK: true, AvailableSpare: 100, AvailableSpareThreshold: 10, WarningTempMinutes: 3}, "Caution"},
	} {
		status, reasons := JudgeHealth(tc.h)
		if status != tc.status {
			t.Errorf("%s: status = %q, want %q", tc.name, status, tc.status)
		}
		if status != "Good" && status != "" && len(reasons) == 0 {
			t.Errorf("%s: %q verdict without any reason", tc.name, status)
		}
	}
}
