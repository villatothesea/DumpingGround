//go:build !windows

package uninstall

// ResolveShortcut has nothing to resolve off Windows.
func ResolveShortcut(path string) string { return "" }
