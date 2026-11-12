package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Regression: the page view column used to keep its natural content height,
// so on short windows the startup detail card ran past the window's bottom
// edge and pushed the status bar off-screen entirely. The view must be
// allowed to shrink (its table scrolls) so the card's rounded bottom edge
// and the status bar stay inside the window.
func TestStartupDetailCardStaysInsideWindow(t *testing.T) {
	note := "Turning it off stops it from starting at sign-in. Nothing is deleted — the entry stays where it is and you can switch it back on."
	for _, h := range []int{760, 640, 580, 540} {
		t.Run(fmt.Sprintf("h=%d", h), func(t *testing.T) {
			m := testModel(t)
			m.page = startupPage
			m.st.items = fakeStartupItems()
			m.st.loaded = true
			m.st.selKey = "run|HKCU|Run|Steam"
			m.st.selected = 0
			tt := ui.NewTester(m.view, 1180, h)
			tt.Frame()
			tt.Frame()
			for _, name := range []string{"Starts from", "Runs", note, "Ready"} {
				r, ok := tt.Find(name)
				if !ok || r.W <= 0 || r.H <= 0 || r.Y+r.H > float32(h)+1 {
					t.Fatalf("h=%d %q bounds=%+v found=%v", h, name, r, ok)
				}
			}
		})
	}
}
