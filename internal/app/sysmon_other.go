//go:build !windows

package app

type systemUsage struct {
	idle, busy uint64
	cpu        float64
	memLoad    uint32
	memTotal   uint64
	memUsed    uint64
}

func (s *systemUsage) sample() {}
