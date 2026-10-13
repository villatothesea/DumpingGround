//go:build !windows

package app

import (
	"os"
	"strings"
)

// systemLang reads the POSIX locale off Windows.
func systemLang() int {
	for _, e := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		loc := strings.ToLower(os.Getenv(e))
		switch {
		case strings.HasPrefix(loc, "zh_tw"), strings.HasPrefix(loc, "zh_hk"), strings.HasPrefix(loc, "zh_hant"):
			return langZhTW
		case strings.HasPrefix(loc, "zh"):
			return langZhCN
		case strings.HasPrefix(loc, "ja"):
			return langJA
		case strings.HasPrefix(loc, "ko"):
			return langKO
		}
	}
	return langEN
}
