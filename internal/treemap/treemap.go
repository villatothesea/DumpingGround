package treemap

import (
	"math"
	"slices"
)

type Item struct {
	ID    int
	Label string
	Value uint64
}

type Rect struct {
	X, Y, W, H float32
	ID         int
}

func (r Rect) Contains(x, y float32) bool {
	return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H
}

// Layout returns a stable squarified treemap. Zero-valued items are skipped.
// The caller owns the returned slice and can reuse it between frames.
func Layout(items []Item, width, height float32) []Rect {
	positive := make([]Item, 0, len(items))
	for _, item := range items {
		if item.Value > 0 {
			positive = append(positive, item)
		}
	}
	slices.SortStableFunc(positive, func(a, b Item) int {
		if a.Value > b.Value {
			return -1
		}
		if a.Value < b.Value {
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	if width <= 0 || height <= 0 || len(positive) == 0 {
		return nil
	}
	total := uint64(0)
	for _, item := range positive {
		total += item.Value
	}
	areas := make([]float64, len(positive))
	for i, item := range positive {
		areas[i] = float64(item.Value) / float64(total) * float64(width) * float64(height)
	}
	result := make([]Rect, 0, len(positive))
	remaining := positive
	remainingAreas := areas
	x, y, w, h := float64(0), float64(0), float64(width), float64(height)
	for len(remaining) > 0 {
		short := min(w, h)
		rowItems := 0
		rowArea := 0.0
		best := math.Inf(1)
		for rowItems < len(remaining) {
			candidate := appendArea(rowArea, remainingAreas[rowItems])
			worse := worst(candidate, short)
			if rowItems > 0 && worse > best {
				break
			}
			rowItems++
			rowArea = candidate
			best = worse
		}
		if rowItems == 0 {
			rowItems = 1
			rowArea = remainingAreas[0]
		}
		if w <= h {
			rowH := rowArea / w
			cursor := x
			for i := 0; i < rowItems; i++ {
				rw := remainingAreas[i] / rowH
				result = append(result, Rect{float32(cursor), float32(y), float32(rw), float32(rowH), remaining[i].ID})
				cursor += rw
			}
			y += rowH
			h -= rowH
		} else {
			rowW := rowArea / h
			cursor := y
			for i := 0; i < rowItems; i++ {
				rh := remainingAreas[i] / rowW
				result = append(result, Rect{float32(x), float32(cursor), float32(rowW), float32(rh), remaining[i].ID})
				cursor += rh
			}
			x += rowW
			w -= rowW
		}
		remaining = remaining[rowItems:]
		remainingAreas = remainingAreas[rowItems:]
		if w <= 0 || h <= 0 {
			break
		}
	}
	return result
}

func appendArea(a, b float64) float64 { return a + b }

func rowSize(area, w, h float64) (float64, float64) {
	if w <= h {
		return w, area / w
	}
	return area / h, h
}

func worst(rowArea, short float64) float64 {
	if rowArea <= 0 || short <= 0 {
		return math.Inf(1)
	}
	// For a row with fixed short side, the largest and smallest item
	// determine the worst aspect ratio.
	return rowArea * rowArea / (short * short)
}

func Hit(rects []Rect, x, y float32) (int, bool) {
	for i := len(rects) - 1; i >= 0; i-- {
		if rects[i].Contains(x, y) {
			return rects[i].ID, true
		}
	}
	return 0, false
}
