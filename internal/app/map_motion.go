package app

import (
	"github.com/dumpingground/dumpingground/internal/treemap"
	"time"
)

const mapTransitionDuration = 320 * time.Millisecond

type mapScene struct {
	Root, Items   int
	Sum           float64
	Width, Height float32
	Rects         []treemap.Rect
}
type mapMotion struct {
	Scene                      mapScene
	From, Previous, PreviousTo []treemap.Rect
	Started                    time.Time
}

// retarget uses the folder's old rectangle as a zoom viewport when descending,
// and the returning folder's destination rectangle when ascending.
func (m *mapMotion) retarget(next mapScene, now time.Time, animate bool, enterID, returnID int) {
	old := m.Scene
	m.From = nil
	m.Previous = nil
	m.PreviousTo = nil
	m.Scene = next
	m.Started = now
	if !animate || len(old.Rects) == 0 || old.Width <= 0 || old.Height <= 0 || next.Width <= 0 || next.Height <= 0 {
		return
	}
	m.Previous = append([]treemap.Rect(nil), old.Rects...)
	full := treemap.Rect{W: next.Width, H: next.Height}
	if anchor, ok := rectByID(old.Rects, enterID); ok && enterID >= 0 {
		for _, r := range next.Rects {
			m.From = append(m.From, projectInto(r, next.Width, next.Height, anchor))
		}
		for _, r := range old.Rects {
			m.PreviousTo = append(m.PreviousTo, projectOut(r, anchor, full.W, full.H))
		}
	} else if anchor, ok := rectByID(next.Rects, returnID); ok && returnID >= 0 {
		for _, r := range next.Rects {
			m.From = append(m.From, projectOut(r, anchor, full.W, full.H))
		}
		for _, r := range old.Rects {
			m.PreviousTo = append(m.PreviousTo, projectInto(r, old.Width, old.Height, anchor))
		}
	} else {
		for _, r := range next.Rects {
			from, ok := rectByID(old.Rects, r.ID)
			if !ok {
				from = r
				from.X += r.W / 2
				from.Y += r.H / 2
				from.W = 0
				from.H = 0
			}
			m.From = append(m.From, from)
		}
		m.PreviousTo = append([]treemap.Rect(nil), old.Rects...)
	}
}
func (m *mapMotion) progress(now time.Time, enabled bool) float32 {
	if !enabled || len(m.From) == 0 {
		return 1
	}
	t := float32(float64(now.Sub(m.Started)) / float64(mapTransitionDuration))
	return min(max(t, 0), 1)
}
func rectByID(rs []treemap.Rect, id int) (treemap.Rect, bool) {
	for _, r := range rs {
		if r.ID == id {
			return r, true
		}
	}
	return treemap.Rect{}, false
}
func interpolateRect(a, b treemap.Rect, t float32) treemap.Rect {
	return treemap.Rect{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t, W: a.W + (b.W-a.W)*t, H: a.H + (b.H-a.H)*t, ID: b.ID}
}
func mapEase(t float32) float32 { return t * t * (3 - 2*t) }
func projectInto(r treemap.Rect, w, h float32, area treemap.Rect) treemap.Rect {
	return treemap.Rect{X: area.X + r.X*area.W/w, Y: area.Y + r.Y*area.H/h, W: r.W * area.W / w, H: r.H * area.H / h, ID: r.ID}
}
func projectOut(r, area treemap.Rect, w, h float32) treemap.Rect {
	if area.W <= 0 || area.H <= 0 {
		return r
	}
	return treemap.Rect{X: (r.X - area.X) * w / area.W, Y: (r.Y - area.Y) * h / area.H, W: r.W * w / area.W, H: r.H * h / area.H, ID: r.ID}
}
