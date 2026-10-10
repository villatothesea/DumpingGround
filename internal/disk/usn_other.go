//go:build !windows

package disk

import (
	"context"
	"errors"
)

// Watcher exists only for the compile-time surface on non-Windows builds.
type Watcher struct{}

func (s *Snapshot) WatchJournal() (*Watcher, error) {
	return nil, errors.New("USN journal requires Windows")
}

func (w *Watcher) Next(context.Context) ([]USNEvent, error) {
	return nil, errors.New("USN journal requires Windows")
}

func (w *Watcher) Close() {}

func defaultUSNStat(string) (uint64, uint64, bool) { return 0, 0, false }
