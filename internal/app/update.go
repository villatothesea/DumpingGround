package app

import (
	"context"
	"net/http"
	"time"

	"github.com/dumpingground/dumpingground/internal/applog"
	"github.com/dumpingground/dumpingground/internal/update"
)

const updatePollInterval = 30 * time.Minute

// updateStatus is the latest release check as the view should render it.
type updateStatus struct {
	Checked  bool
	Checking bool
	Newer    bool
	Version  string
	URL      string
	Err      string
	At       time.Time
}

// watchUpdates owns the release-check loop. It only polls while the opt-in
// setting is on; manual requests arrive through updateNow at any time.
func (m *Model) watchUpdates() {
	if m.updateAuto.Load() {
		m.performUpdateCheck()
	}
	ticker := time.NewTicker(updatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if m.updateAuto.Load() {
				m.performUpdateCheck()
			}
		case <-m.updateNow:
			m.performUpdateCheck()
		case <-m.ctx.Done():
			return
		}
	}
}

// requestUpdateCheck queues a manual check without blocking the view.
func (m *Model) requestUpdateCheck() {
	st := m.updateState()
	if st.Checking {
		return
	}
	st.Checking = true
	m.updateResult.Store(st)
	m.invalidate()
	select {
	case m.updateNow <- struct{}{}:
	default:
	}
}

func (m *Model) performUpdateCheck() {
	if !m.updateBusy.CompareAndSwap(false, true) {
		return
	}
	defer m.updateBusy.Store(false)
	st := m.updateState()
	st.Checking = true
	m.updateResult.Store(st)
	m.invalidate()

	url := m.updateURL
	if url == "" {
		url = update.LatestURL
	}
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	rel, err := update.Latest(ctx, http.DefaultClient, url)
	cancel()

	st = updateStatus{Checked: true, At: time.Now()}
	if err != nil {
		st.Err = err.Error()
		applog.Logf("update check: %v", err)
	} else {
		st.Version = rel.Tag
		st.URL = rel.URL
		st.Newer = update.Newer(version, rel.Tag)
	}
	m.updateResult.Store(st)
	m.invalidate()
}

func (m *Model) updateState() updateStatus {
	if v := m.updateResult.Load(); v != nil {
		return v.(updateStatus)
	}
	return updateStatus{}
}

// invalidate wakes the view after a worker changes update state; an empty
// posted callback is enough to run the frame on the UI thread.
func (m *Model) invalidate() {
	m.post(func() {
		if m.window != nil {
			m.window.Invalidate()
		}
	})
}
