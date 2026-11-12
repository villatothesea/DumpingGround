package app

import (
	"reflect"
	"testing"
)

type testWindow struct {
	minimized bool
	calls     []string
}

func (w *testWindow) IsMinimized() bool { return w.minimized }
func (w *testWindow) Restore()          { w.calls = append(w.calls, "restore") }
func (w *testWindow) Show()             { w.calls = append(w.calls, "show") }
func (w *testWindow) Focus()            { w.calls = append(w.calls, "focus") }

func TestRevealWindowRestoresOnlyMinimized(t *testing.T) {
	minimized := &testWindow{minimized: true}
	revealWindow(minimized)
	if want := []string{"restore", "focus"}; !reflect.DeepEqual(minimized.calls, want) {
		t.Fatalf("minimized calls = %v, want %v", minimized.calls, want)
	}

	// A visible maximized window is shown and focused without Restore:
	// SW_RESTORE would turn it back into its small normal rectangle.
	visible := &testWindow{}
	revealWindow(visible)
	if want := []string{"show", "focus"}; !reflect.DeepEqual(visible.calls, want) {
		t.Fatalf("visible calls = %v, want %v", visible.calls, want)
	}
}
