package hwmon

import "testing"

// sataData builds a 512-byte SMART READ DATA buffer from a set of
// attributes (the reply embeds 30 entries at byte 2).
func sataData(attrs map[int]struct {
	cur, worst, raw uint64
}) []byte {
	b := make([]byte, 512)
	i := 0
	for id, a := range attrs {
		e := b[2+i*12:]
		e[0] = byte(id)
		e[3], e[4] = byte(a.cur), byte(a.worst)
		for k := 0; k < 6; k++ {
			e[5+k] = byte(a.raw >> (8 * k))
		}
		i++
	}
	return b
}

func TestParseSATAHealth(t *testing.T) {
	data := sataData(map[int]struct {
		cur, worst, raw uint64
	}{
		5:   {100, 100, 0},
		9:   {99, 99, 12000},
		12:  {99, 99, 900},
		194: {60, 40, 34},
		197: {100, 100, 0},
		198: {100, 100, 0},
		231: {96, 96, 0},
		241: {99, 99, 123456789},
	})
	h := parseSATAHealth(data, nil)
	if h == nil {
		t.Fatal("a valid attribute table failed to parse")
	}
	if h.PowerOnHours != 12000 || h.PowerCycles != 900 || h.Temperature != 34 {
		t.Errorf("hours/cycles/temp = %d/%d/%d", h.PowerOnHours, h.PowerCycles, h.Temperature)
	}
	if h.LifeLeft != 96 || h.TotalWritten != 123456789 {
		t.Errorf("life/written = %d/%d", h.LifeLeft, h.TotalWritten)
	}
	if status, _ := JudgeSATAHealth(h); status != "Good" {
		t.Errorf("healthy drive judged %q", status)
	}
	if h := parseSATAHealth(make([]byte, 64), nil); h != nil {
		t.Error("a truncated table parsed as valid")
	}
}

func TestJudgeSATAHealth(t *testing.T) {
	mk := func(a map[int]struct {
		cur, worst, raw uint64
	}) *SATAHealth {
		return parseSATAHealth(sataData(a), nil)
	}
	for _, tc := range []struct {
		name   string
		h      *SATAHealth
		status string
	}{
		{"no reading", nil, ""},
		{"pending sectors", mk(map[int]struct {
			cur, worst, raw uint64
		}{197: {100, 100, 3}}), "Replace"},
		{"offline uncorrectable", mk(map[int]struct {
			cur, worst, raw uint64
		}{198: {100, 100, 1}}), "Replace"},
		{"reallocated sectors", mk(map[int]struct {
			cur, worst, raw uint64
		}{5: {90, 90, 2}}), "Caution"},
		{"life nearly gone", mk(map[int]struct {
			cur, worst, raw uint64
		}{231: {8, 8, 0}}), "Caution"},
		{"all clear", mk(map[int]struct {
			cur, worst, raw uint64
		}{5: {100, 100, 0}, 231: {96, 96, 0}}), "Good"},
	} {
		status, reasons := JudgeSATAHealth(tc.h)
		if status != tc.status {
			t.Errorf("%s: status = %q, want %q", tc.name, status, tc.status)
		}
		if status != "Good" && status != "" && len(reasons) == 0 {
			t.Errorf("%s: %q verdict without a reason", tc.name, status)
		}
	}

	// An attribute at or below its own failure threshold is a Replace.
	thr := make([]byte, 512)
	thr[2], thr[2+3] = 5, 10
	h := parseSATAHealth(sataData(map[int]struct {
		cur, worst, raw uint64
	}{5: {10, 10, 0}}), thr)
	if !h.BelowThreshold {
		t.Error("an attribute at its threshold was not flagged")
	}
	if status, _ := JudgeSATAHealth(h); status != "Replace" {
		t.Errorf("below-threshold drive judged %q", status)
	}
}
