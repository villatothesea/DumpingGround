//go:build !windows

package disk

func Volumes() []Volume { return nil }
