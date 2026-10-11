//go:build !windows

package uninstall

// enumUninstall is a no-op off Windows — the report only runs there.
func enumUninstall() []Entry { return nil }
