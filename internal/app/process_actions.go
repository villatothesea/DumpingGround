package app

import "github.com/dumpingground/dumpingground/internal/process"

func (m *Model) requestHandleSearch(pid uint32) {
	inspector, ok := m.processSvc.Handles()
	if !ok {
		m.handleErr = "Global handle enumeration unavailable"
		return
	}
	m.handleLoading = true
	m.handleErr = "Searching system handles…"
	query := m.handleQuery
	go func() {
		items, err := inspector.SearchHandles(pid, query, 5000)
		m.post(func() {
			if m.inspectedPID != pid {
				return
			}
			m.handles = items
			m.handleErr = ""
			m.handleLoading = false
			if err != nil {
				m.handleErr = err.Error()
			}
		})
	}()
}

func (m *Model) requestStack(pid, threadID uint32) {
	inspector, ok := m.processSvc.Stacks()
	if !ok {
		m.stackErr = "Call stack capture unavailable"
		return
	}
	m.stackThread = threadID
	m.stackLoading = true
	m.stackErr = "Capturing thread context…"
	go func() {
		frames, err := inspector.Stack(pid, threadID, 64)
		m.post(func() {
			if m.inspectedPID != pid {
				return
			}
			if m.stacks == nil {
				m.stacks = map[uint32][]process.StackFrame{}
			}
			m.stacks[threadID] = frames
			m.stackLoading = false
			m.stackErr = ""
			if err != nil {
				m.stackErr = err.Error()
			}
		})
	}()
}
