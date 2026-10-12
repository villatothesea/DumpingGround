//go:build !windows

package elevwork

// Serve exists so cmd/dumpingground builds everywhere; the worker is
// Windows-only.
func Serve(parentPID uint32, pipeName string) int { return 2 }
