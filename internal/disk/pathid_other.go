//go:build !windows

package disk

// PathID is a no-op off Windows — junk cleanup and leftover removal
// only run there.
type PathID struct{}

// BindPath always succeeds on platforms without cleanup features.
func BindPath(string, bool) (*PathID, error) { return &PathID{}, nil }

// Verify is vacuous where BindPath pinned nothing.
func (*PathID) Verify() bool { return true }
