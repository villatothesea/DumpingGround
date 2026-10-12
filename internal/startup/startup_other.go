//go:build !windows

package startup

// List has nothing to read off Windows.
func List() []Item { return nil }

// SetEnabled is Windows-only.
func SetEnabled(it Item, on bool) error { return ErrUnsupported }

// OpenInRegedit is Windows-only.
func OpenInRegedit(it Item) error { return ErrUnsupported }
