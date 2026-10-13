package app

import (
	"os"
	"testing"
)

// TestMain pins the UI language to English: this developer machine runs
// a zh-CN Windows, so "System" would render every page in Chinese and
// the tests asserting English labels would fail.
func TestMain(m *testing.M) {
	langOverride.Store(int32(langEN))
	curLang.Store(int32(langEN))
	os.Exit(m.Run())
}
