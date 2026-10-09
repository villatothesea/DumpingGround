package treemap

import "slices"

// TreeLayout groups files inside their directory's area. Budget limits the
// number of painted rectangles. A directory becomes one drillable rectangle
// when its children would be smaller than pixels or exceed that budget.
// children must return nil for a file; directory weights are already summed.
func TreeLayout(root int, width, height float32, budget int, children func(int) []Item) []Rect {
	if width <= 0 || height <= 0 || budget < 1 {
		return nil
	}
	out := make([]Rect, 0, min(budget, 1024))
	var visit func(int, Rect, int, int)
	visit = func(id int, bounds Rect, quota, depth int) {
		if quota <= 1 || bounds.W*bounds.H < 16 || depth > 256 {
			bounds.ID = id
			out = append(out, bounds)
			return
		}
		items := children(id)
		if len(items) == 0 {
			bounds.ID = id
			out = append(out, bounds)
			return
		}
		positive := make([]Item, 0, len(items))
		for _, it := range items {
			if it.Value > 0 {
				positive = append(positive, it)
			}
		}
		if len(positive) == 0 {
			return
		}
		slices.SortStableFunc(positive, func(a, b Item) int {
			if a.Value > b.Value {
				return -1
			}
			if a.Value < b.Value {
				return 1
			}
			return 0
		})
		grouped := false
		if len(positive) > quota {
			var other uint64
			for _, it := range positive[quota-1:] {
				other += it.Value
			}
			positive = append(positive[:quota-1], Item{ID: id, Value: other})
			grouped = true
		}
		rects := Layout(positive, bounds.W, bounds.H)
		spare := quota - len(rects)
		for _, r := range rects {
			r.X += bounds.X
			r.Y += bounds.Y
			if grouped && r.ID == id {
				out = append(out, r)
				continue
			}
			share := 1 + int(float64(spare)*float64(r.W*r.H)/float64(bounds.W*bounds.H))
			visit(r.ID, r, share, depth+1)
		}
	}
	visit(root, Rect{W: width, H: height, ID: root}, budget, 0)
	return out
}
