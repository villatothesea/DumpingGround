package app

import (
	"fmt"
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestIconDebug verifies icon glyphs are centered in icon-only buttons and
// renders the app icon when DG_ICON_OUT is set.
func TestIconDebug(t *testing.T) {
	m := testModel(t)
	view := func(c *ui.Context) {
		t := c.Theme()
		r := ui.Row(c).Gap(8).Padding(8).Height(64).AlignItems(ui.Center).Background(t.Background)
		r.Children(func() {
			m.iconButton(c, "moon", "btn-moon")
			m.iconButton(c, "gear", "btn-gear")
			m.pactButton(c, "up", "btn-up")
			m.iconTextButton(c, "x", "End process")
		})
	}
	tt := ui.NewTester(view, 480, 80)
	tt.Frame()
	if dest := os.Getenv("DG_SHOTS"); dest != "" {
		dump(t, tt, dest, "icon-debug.png")
	}
	img := tt.Image()
	for _, s := range []string{"btn-moon", "btn-gear", "btn-up"} {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("no %s", s)
		}
		x0, y0 := int(r.X), int(r.Y)
		x1, y1 := int(r.X+r.W), int(r.Y+r.H)
		minX, minY, maxX, maxY := x1, y1, x0, y0
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				pr, pg, pb, _ := img.At(x, y).RGBA()
				if pr>>8 < 100 && pg>>8 < 100 && pb>>8 < 100 {
					minX, maxX = min(minX, x), max(maxX, x)
					minY, maxY = min(minY, y), max(maxY, y)
				}
			}
		}
		// The glyph's center must be within 3px of the button's center.
		cx, cy := (minX+maxX)/2, (minY+maxY)/2
		bx, by := int(r.X+r.W/2), int(r.Y+r.H/2)
		fmt.Printf("%s glyph (%d-%d,%d-%d) center %d,%d button center %d,%d\n", s, minX, maxX, minY, maxY, cx, cy, bx, by)
		if dx, dy := cx-bx, cy-by; dx < -3 || dx > 3 || dy < -3 || dy > 3 {
			t.Errorf("%s glyph center %d,%d is off button center %d,%d", s, cx, cy, bx, by)
		}
	}
}

// TestRenderAppIcon renders the product icon once for embedding; run with
// DG_ICON_OUT set to the PNG destination.
func TestRenderAppIcon(t *testing.T) {
	dest := os.Getenv("DG_ICON_OUT")
	if dest == "" {
		t.Skip("DG_ICON_OUT not set")
	}
	view := func(c *ui.Context) {
		t := c.Theme()
		ui.Box(c).Size(64, 64).Radius(14).Background(t.Accent).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Icon(glyphs["logo"], ui.Rect{X: r.X + 12, Y: r.Y + 12, W: 40, H: 40}, t.AccentText)
		})
	}
	tt := ui.NewTester(view, 64, 64)
	tt.Frame()
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
