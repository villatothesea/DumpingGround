package app

import (
	"image"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCJKRendering proves the text engine shapes CJK — DirectWrite's
// system font fallback should map Han, kana and hangul onto real fonts.
// A missing fallback shows up as zero-width or clipped runs.
func TestCJKRendering(t *testing.T) {
	lines := []string{
		"简体中文：磁盘清理已释放空间",
		"繁體中文：磁碟清理已釋放空間",
		"日本語：ディスククリーンアップ",
		"한국어: 디스크 정리 완료",
		"Mixed: 清理 2.5 GB · GPU 42%",
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Padding(16).Gap(8).Fill().Children(func() {
			for _, s := range lines {
				ui.Text(c, s)
			}
		})
	}, 480, 200)
	tt.Frame()

	for _, s := range lines {
		r, ok := tt.Find(s)
		if !ok {
			t.Errorf("text %q not found", s)
			continue
		}
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("text %q laid out empty: %+v", s, r)
		}
	}

	// The text rows must actually paint ink — an empty glyph run would
	// leave the whole band background-colored.
	img := tt.Image()
	bg := img.RGBAAt(4, 4)
	ink := 0
	bounds := image.Rect(0, 0, 480, 200)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p := img.RGBAAt(x, y)
			dr, dg, db := int(p.R)-int(bg.R), int(p.G)-int(bg.G), int(p.B)-int(bg.B)
			if dr < 0 {
				dr = -dr
			}
			if dg < 0 {
				dg = -dg
			}
			if db < 0 {
				db = -db
			}
			if dr+dg+db > 60 {
				ink++
			}
		}
	}
	if ink < 2000 {
		t.Fatalf("CJK text painted only %d inked pixels — glyphs missing?", ink)
	}
	if dest := os.Getenv("DG_SHOTS"); dest != "" {
		dump(t, tt, dest, "cjk.png")
	}
}
