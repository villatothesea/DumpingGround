//go:build windows

package startup

import (
	"os"
	"testing"

	"github.com/dumpingground/dumpingground/internal/appdir"
	"github.com/dumpingground/dumpingground/internal/elevate"
)

// Live toggle against the hardened Codex sandbox service — only runs
// when DG_LIVE_SVC is set, and only meaningful elevated.
func TestHardenedServiceToggle(t *testing.T) {
	if os.Getenv("DG_LIVE_SVC") == "" {
		t.Skip("set DG_LIVE_SVC to run")
	}
	if !elevate.SelfElevated() {
		t.Skip("requires an elevated test process")
	}
	if d := os.Getenv("DG_APPDIR"); d != "" {
		appdir.SetDir(d)
		defer appdir.SetDir("")
	}
	var it *Item
	items := List()
	for i := range items {
		if items[i].Kind == KindService && items[i].Name == "ChatGPT" {
			it = &items[i]
		}
	}
	if it == nil {
		t.Skip("ChatGPT service not present")
	}
	t.Logf("before: enabled=%v trigger=%s", it.Enabled, it.Trigger)
	if err := SetEnabled(*it, os.Getenv("DG_SVC_ON") == "1"); err != nil {
		t.Fatalf("toggle failed: %v", err)
	}
	t.Log("toggle ok")
}
