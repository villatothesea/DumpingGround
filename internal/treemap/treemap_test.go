package treemap

import "testing"

func TestLayoutCoversArea(t *testing.T) {
	r := Layout([]Item{{ID: 1, Value: 60}, {ID: 2, Value: 30}, {ID: 3, Value: 10}}, 100, 50)
	if len(r) != 3 {
		t.Fatalf("got %d rectangles", len(r))
	}
	for _, x := range r {
		if x.W <= 0 || x.H <= 0 || x.X < 0 || x.Y < 0 || x.X+x.W > 100.01 || x.Y+x.H > 50.01 {
			t.Fatalf("rectangle outside bounds: %+v", x)
		}
	}
	if id, ok := Hit(r, r[0].X+r[0].W/2, r[0].Y+r[0].H/2); !ok || id != r[0].ID {
		t.Fatalf("hit = %d, %v", id, ok)
	}
}

func TestLayoutIgnoresZero(t *testing.T) {
	if got := Layout([]Item{{ID: 1}}, 0, 50); got != nil {
		t.Fatal("expected no layout for zero width")
	}
}
