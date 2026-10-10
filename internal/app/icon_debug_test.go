package app

import (
	"bytes"
	"encoding/binary"
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

// appIconView draws the product icon (accent tile plus the logo glyph)
// at a given pixel size.
func appIconView(size float32) func(c *ui.Context) {
	return func(c *ui.Context) {
		t := c.Theme()
		pad := size * 12 / 64
		ui.Box(c).Size(size, size).Radius(size * 14 / 64).Background(t.Accent).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Icon(glyphs["logo"], ui.Rect{X: r.X + pad, Y: r.Y + pad, W: size - 2*pad, H: size - 2*pad}, t.AccentText)
		})
	}
}

func renderIconPNG(size int) ([]byte, error) {
	tt := ui.NewTester(appIconView(float32(size)), size, size)
	tt.Frame()
	var buf bytes.Buffer
	if err := png.Encode(&buf, tt.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TestRenderAppIcon renders the product icon once for embedding; run with
// DG_ICON_OUT set to the PNG destination, DG_ICO_OUT to write a
// multi-size .ico for the executable's resources.
func TestRenderAppIcon(t *testing.T) {
	if dest := os.Getenv("DG_ICON_OUT"); dest != "" {
		data, err := renderIconPNG(256)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	dest := os.Getenv("DG_ICO_OUT")
	if dest == "" {
		t.Skip("DG_ICO_OUT not set")
	}
	sizes := []int{16, 24, 32, 48, 64, 256}
	pngs := make([][]byte, len(sizes))
	for i, s := range sizes {
		var err error
		if pngs[i], err = renderIconPNG(s); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint16(0))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(len(pngs)))
	offset := 6 + 16*len(pngs)
	for i, e := range pngs {
		sz := byte(sizes[i])
		if sizes[i] >= 256 {
			sz = 0
		}
		out.WriteByte(sz)
		out.WriteByte(sz)
		out.WriteByte(0)
		out.WriteByte(0)
		binary.Write(&out, binary.LittleEndian, uint16(1))
		binary.Write(&out, binary.LittleEndian, uint16(32))
		binary.Write(&out, binary.LittleEndian, uint32(len(e)))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(e)
	}
	for _, e := range pngs {
		out.Write(e)
	}
	if err := os.WriteFile(dest, out.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}
