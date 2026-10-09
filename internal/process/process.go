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
	ReadRate   uint64
	WriteRate  uint64
	Threads    uint32
	Handles    uint32
	Priority   uint32
	Started    time.Time
	Elevated   bool
	Access     string
}
type Module struct {
	Name, Path string
	Size       uint64
}
type Thread struct {
	ID       uint32
	OwnerPID uint32
	Priority int32
}
type Handle struct {
	PID        uint32
	Value      uintptr
	Type, Name string
	Access     uint32
	Attributes uint32
}
type StackFrame struct {
	ThreadID       uint32
	Address        uint64
	Module, Symbol string
	Offset         uint64
}
type Inspector interface {
	Modules(pid uint32) ([]Module, error)
	Threads(pid uint32) ([]Thread, error)
}
type PropertyInspector interface {
	Properties(pid uint32) (Info, error)
}
type HandleInspector interface {
	SearchHandles(pid uint32, query string, limit int) ([]Handle, error)
}
type StackInspector interface {
	Stack(pid, threadID uint32, maxFrames int) ([]StackFrame, error)
}
type Collector interface {
	Snapshot() ([]Info, error)
	Terminate(pid uint32, tree bool) error
}
type Service struct{ collector Collector }

func NewService() *Service                               { return &Service{collector: newCollector()} }
func (s *Service) Snapshot() ([]Info, error)             { return s.collector.Snapshot() }
func (s *Service) Terminate(pid uint32, tree bool) error { return s.collector.Terminate(pid, tree) }
func (s *Service) TerminateInfo(info Info, tree bool) error {
	if c, ok := s.collector.(interface{ TerminateChecked(Info, bool) error }); ok {
		return c.TerminateChecked(info, tree)
	}
	return s.collector.Terminate(info.PID, tree)
}
func (s *Service) Inspect() (Inspector, bool) { v, ok := s.collector.(Inspector); return v, ok }
func (s *Service) Handles() (HandleInspector, bool) {
	v, ok := s.collector.(HandleInspector)
	return v, ok
}
func (s *Service) Stacks() (StackInspector, bool) {
	v, ok := s.collector.(StackInspector)
	return v, ok
}
