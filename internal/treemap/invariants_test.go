package treemap

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestLayoutConservesAreaAndNeverOverlaps(t *testing.T) {
	for n := 1; n <= 100; n += 7 {
		items := make([]Item, n)
		total := 0.0
		for i := range items {
			items[i] = Item{ID: i, Value: uint64(rand.IntN(10000) + 1)}
			total += float64(items[i].Value)
		}
		rects := Layout(items, 641, 379)
		if len(rects) != n {
			t.Fatalf("rectangles=%d items=%d", len(rects), n)
		}
		sum := 0.0
		for i, r := range rects {
			area := float64(r.W * r.H)
			sum += area
			expected := 641 * 379 * float64(items[r.ID].Value) / total
			if math.Abs(area-expected) > expected*0.001+0.1 {
				t.Fatalf("wrong area %v: %f want %f", r, area, expected)
			}
			if r.X < 0 || r.Y < 0 || r.X+r.W > 641.01 || r.Y+r.H > 379.01 {
				t.Fatalf("outside: %v", r)
			}
			for _, s := range rects[i+1:] {
				overW := min(r.X+r.W, s.X+s.W) - max(r.X, s.X)
				overH := min(r.Y+r.H, s.Y+s.H) - max(r.Y, s.Y)
				if overW > 0.01 && overH > 0.01 {
					t.Fatalf("overlap: %v %v", r, s)
				}
			}
		}
		if math.Abs(sum-641*379) > 2 {
			t.Fatalf("area sum=%f", sum)
		}
	}
}
func TestLargeWeightsDoNotOverflow(t *testing.T) {
	r := Layout([]Item{{ID: 1, Value: math.MaxUint64}, {ID: 2, Value: math.MaxUint64}}, 100, 100)
	if len(r) != 2 || math.Abs(float64(r[0].W*r[0].H)-5000) > 1 {
		t.Fatal(r)
	}
}
func BenchmarkLayout(b *testing.B) {
	items := make([]Item, 1600)
	for i := range items {
		items[i] = Item{ID: i, Value: uint64(i + 1)}
	}
	b.ReportAllocs()
	for b.Loop() {
		Layout(items, 1000, 400)
	}
}
