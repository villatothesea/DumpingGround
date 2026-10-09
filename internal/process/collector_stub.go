//go:build !windows

package process

import "errors"

type unavailableCollector struct{}

func newCollector() Collector { return unavailableCollector{} }
func (unavailableCollector) Snapshot() ([]Info, error) {
	return nil, errors.New("process collection is only available on Windows")
}
func (unavailableCollector) Terminate(uint32, bool) error {
	return errors.New("process control is only available on Windows")
}
