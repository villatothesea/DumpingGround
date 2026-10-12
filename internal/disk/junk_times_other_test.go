//go:build !windows

package disk

import (
	"os"
	"testing"
	"time"
)

// setAllFileTimes ages a fixture file — off Windows the age rule
// reads mtime only, so Chtimes is enough.
func setAllFileTimes(t *testing.T, path string, tm time.Time) {
	t.Helper()
	if err := os.Chtimes(path, tm, tm); err != nil {
		t.Fatal(err)
	}
}
