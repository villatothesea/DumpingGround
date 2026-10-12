package app

import (
	"math"
	"time"

	"github.com/dumpingground/dumpingground/internal/hwmon"
	"github.com/egoist/mygo/ui"
)

// chartWindow is the span every live chart shows; hwKeep is how much
// history the sampler retains (a little more, so the line enters from
// beyond the left edge instead of starting mid-chart).
const (
	chartWindow = 60 * time.Second
	hwKeep      = 125
	// A gap longer than this between two samples (a minimized window, a
	// sleep) breaks the line rather than drawing a false ramp across it.
	chartGap = 2500 * time.Millisecond
)

// hwLine is one series on a chart: val reads it from a sample, ok=false
// marks "no data this tick".
type hwLine struct {
	val  func(*hwmon.Sample) (float64, bool)
	col  ui.Color
	fill bool
}

type chartOpts struct {
	// max fixes the top of the scale; 0 autoscales to the visible data
	// with floor as its minimum.
	max, floor float64
	grid       bool
	col        ui.Color // frame and background tint
	lines      []hwLine
}

// niceCeil rounds v up to 1, 2 or 5 × a power of ten — the scale a rate
// chart climbs through as traffic grows, so the axis does not jitter.
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(v)))
	switch f := v / exp; {
	case f <= 1:
		return exp
	case f <= 2:
		return 2 * exp
	case f <= 5:
		return 5 * exp
	}
	return 10 * exp
}

// chartScale returns the top of the y axis for the visible samples.
func chartScale(hist []hwmon.Sample, o chartOpts) float64 {
	if o.max > 0 {
		return o.max
	}
	top := o.floor
	if len(hist) > 0 {
		newest := hist[len(hist)-1].At
		for i := range hist {
			if newest.Sub(hist[i].At) > chartWindow {
				continue
			}
			for _, l := range o.lines {
				if v, ok := l.val(&hist[i]); ok && v > top {
					top = v
				}
			}
		}
	}
	return niceCeil(top)
}

// drawChart paints the rolling window ending at the newest sample: a tinted
// face, a scrolling grid, then each series as a filled line. Samples sit at
// their timestamps, so a stall shows as a gap, not a stretched line.
func drawChart(p *ui.Painter, r ui.Rect, t *ui.Theme, hist []hwmon.Sample, o chartOpts) {
	if r.W < 4 || r.H < 4 {
		return
	}
	p.Fill(r, o.col.Alpha(0.06), 4)
	p.Clip(r, 4, func() {
		var newest time.Time
		if len(hist) > 0 {
			newest = hist[len(hist)-1].At
		}
		x := func(at time.Time) float32 {
			return r.X + r.W*float32(1-newest.Sub(at).Seconds()/chartWindow.Seconds())
		}
		top := chartScale(hist, o)
		y := func(v float64) float32 {
			return r.Y + r.H - 1 - float32(max(0, min(1, v/top)))*(r.H-2)
		}
		grid := t.TextMuted.Alpha(0.14)
		if o.grid {
			for _, f := range []float32{0.25, 0.5, 0.75} {
				p.Line(r.X, r.Y+r.H*f, r.X+r.W, r.Y+r.H*f, 1, grid)
			}
		}
		if !newest.IsZero() {
			// Vertical lines every 10 s, anchored to the wall clock so
			// they scroll left with the data instead of standing still.
			step := 10 * time.Second
			for at := newest.Truncate(step); newest.Sub(at) <= chartWindow; at = at.Add(-step) {
				gx := x(at)
				p.Line(gx, r.Y, gx, r.Y+r.H, 1, grid)
			}
		}
		base := r.Y + r.H - 1
		for _, l := range o.lines {
			var seg []ui.Rect // x,y packed as Rect{X,Y} points of the current run
			flush := func() {
				if len(seg) >= 2 {
					var line ui.Path
					line.MoveTo(seg[0].X, seg[0].Y)
					for _, pt := range seg[1:] {
						line.LineTo(pt.X, pt.Y)
					}
					if l.fill {
						var area ui.Path
						area.MoveTo(seg[0].X, base)
						for _, pt := range seg {
							area.LineTo(pt.X, pt.Y)
						}
						area.LineTo(seg[len(seg)-1].X, base).Close()
						p.FillPath(&area, l.col.Alpha(0.22))
					}
					p.StrokePath(&line, 1.5, l.col)
				}
				seg = seg[:0]
			}
			var prev time.Time
			for i := range hist {
				s := &hist[i]
				if newest.Sub(s.At) > chartWindow+2*time.Second {
					continue
				}
				v, ok := l.val(s)
				if !ok || (!prev.IsZero() && s.At.Sub(prev) > chartGap) {
					flush()
				}
				if ok {
					seg = append(seg, ui.Rect{X: x(s.At), Y: y(v)})
					prev = s.At
				}
			}
			flush()
		}
	})
	p.Stroke(r, o.col.Alpha(0.55), 4, 1)
}
