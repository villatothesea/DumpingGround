package process

import "time"

type Info struct {
	PID        uint32
	PPID       uint32
	Name       string
	Path       string
	Command    string
	User       string
	CPU        float64
	Memory     uint64
	Private    uint64
	ReadBytes  uint64
	WriteBytes uint64
	Threads    uint32
	Started    time.Time
	Elevated   bool
}

type Collector interface {
	Snapshot() ([]Info, error)
	Terminate(pid uint32, tree bool) error
}

type Service struct{ collector Collector }

func NewService() *Service                               { return &Service{collector: newCollector()} }
func (s *Service) Snapshot() ([]Info, error)             { return s.collector.Snapshot() }
func (s *Service) Terminate(pid uint32, tree bool) error { return s.collector.Terminate(pid, tree) }
