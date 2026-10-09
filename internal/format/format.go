package format

import "fmt"

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
