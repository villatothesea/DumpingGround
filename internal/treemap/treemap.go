// Package treemap implements a UI-independent squarified area layout.
package treemap

import (
	"cmp"
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

func (r Rect) Contains(x, y float32) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

// Layout sorts a copy of the input. Floating point summation avoids overflowing
// when several very large uint64 values are supplied. All output is local to
// the given bounds, and zero weights consume no area.
func Layout(items []Item, width, height float32) []Rect {
	if width <= 0 || height <= 0 {
		return nil
	}
	a := make([]Item, 0, len(items))
	total := 0.0
	for _, it := range items {
		if it.Value > 0 {
			a = append(a, it)
			total += float64(it.Value)
		}
	}
	if total == 0 {
		return nil
	}
	slices.SortFunc(a, func(x, y Item) int {
		if n := cmp.Compare(y.Value, x.Value); n != 0 {
			return n
		}
		return cmp.Compare(x.ID, y.ID)
	})
	scale := float64(width) * float64(height) / total
	areas := make([]float64, len(a))
	for i := range a {
		areas[i] = float64(a[i].Value) * scale
	}
	result := make([]Rect, 0, len(a))
	x, y, w, h := 0.0, 0.0, float64(width), float64(height)
	for start := 0; start < len(a) && w > 0 && h > 0; {
		short := math.Min(w, h)
		end := start + 1
		sum := areas[start]
		score := aspect(sum, areas[start], areas[start], short)
		for end < len(a) {
			next := sum + areas[end]
			s := aspect(next, areas[start], areas[end], short)
			if s > score {
				break
			}
			sum = next
			score = s
			end++
		}
		if w >= h {
			thickness := math.Min(w, sum/h)
			cursor := y
			for i := start; i < end; i++ {
				length := areas[i] / thickness
				if i == end-1 {
					length = y + h - cursor
				}
				result = append(result, Rect{float32(x), float32(cursor), float32(thickness), float32(length), a[i].ID})
				cursor += length
			}
			x += thickness
			w -= thickness
		} else {
			thickness := math.Min(h, sum/w)
			cursor := x
			for i := start; i < end; i++ {
				length := areas[i] / thickness
				if i == end-1 {
					length = x + w - cursor
				}
				result = append(result, Rect{float32(cursor), float32(y), float32(length), float32(thickness), a[i].ID})
				cursor += length
			}
			y += thickness
			h -= thickness
		}
		start = end
	}
	return result
}
func aspect(sum, largest, smallest, side float64) float64 {
	sq := sum * sum
	s := side * side
	return math.Max(s*largest/sq, sq/(s*smallest))
}
func Hit(rs []Rect, x, y float32) (int, bool) {
	for _, r := range rs {
		if r.Contains(x, y) {
			return r.ID, true
		}
	}
	return 0, false
}
