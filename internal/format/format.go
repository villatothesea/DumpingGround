package format

import (
	"fmt"
	"strings"
)

func Bytes(v uint64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	value := float64(v)
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	for _, u := range units {
		value /= unit
		if value < unit || u == "PB" {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%d B", v)
}

func Percent(v float64) string { return fmt.Sprintf("%.1f%%", v) }

func Rate(v uint64) string { return Bytes(v) + "/s" }

// Int groups an integer's digits in thousands: 1234567 → "1,234,567".
func Int(v uint64) string {
	s := fmt.Sprint(v)
	n := len(s)
	if n <= 3 {
		return s
	}
	var b strings.Builder
	b.Grow(n + n/3)
	for i, ch := range s {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	return b.String()
}
