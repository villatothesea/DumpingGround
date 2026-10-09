package treemap

import (
	"math"
	"testing"
)

func TestTreeLayoutGroupsDescendantsAndBoundsWork(t *testing.T) {
	tree := map[int][]Item{0: {{ID: 1, Value: 80}, {ID: 2, Value: 20}}, 1: {{ID: 3, Value: 50}, {ID: 4, Value: 30}}}
	rs := TreeLayout(0, 100, 100, 100, func(id int) []Item { return tree[id] })
	if len(rs) != 3 {
		t.Fatalf("got %v", rs)
	}
	expected := map[int]float64{2: 2000, 3: 5000, 4: 3000}
	for _, r := range rs {
		if math.Abs(float64(r.W*r.H)-expected[r.ID]) > 1 {
			t.Fatalf("wrong area %+v", r)
		}
	}
	rs = TreeLayout(0, 100, 100, 2, func(id int) []Item { return tree[id] })
	if len(rs) > 2 {
		t.Fatal("budget exceeded")
	}
	found := false
	for _, r := range rs {
		if r.ID == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("directory was not retained as drillable aggregate")
	}
	many := make([]Item, 10000)
	for i := range many {
		many[i] = Item{ID: i + 1, Value: 1}
	}
	rs = TreeLayout(0, 1000, 500, 100, func(id int) []Item {
		if id == 0 {
			return many
		}
		return nil
	})
	if len(rs) > 100 {
		t.Fatalf("too many rectangles: %d", len(rs))
	}
	area := float64(0)
	for _, r := range rs {
		area += float64(r.W * r.H)
	}
	if math.Abs(area-500000) > 10 {
		t.Fatal(area)
	}
}
