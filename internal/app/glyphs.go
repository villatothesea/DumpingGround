package app

import "github.com/egoist/mygo/ui"

// Glyph paths come from the ui-redesign prototype (24×24, stroke icons).
var glyphs = map[string]*ui.SVG{
	"sun":     glyph(`<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>`),
	"moon":    glyph(`<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>`),
	"gear":    glyph(`<path d="M12.2 2h-.4a2 2 0 0 0-2 2v.2a2 2 0 0 1-1 1.7l-.4.2a2 2 0 0 1-2 0l-.2-.1a2 2 0 0 0-2.7.7l-.2.4a2 2 0 0 0 .7 2.7l.2.1a2 2 0 0 1 1 1.7v.5a2 2 0 0 1-1 1.7l-.2.1a2 2 0 0 0-.7 2.7l.2.4a2 2 0 0 0 2.7.7l.2-.1a2 2 0 0 1 2 0l.4.2a2 2 0 0 1 1 1.7v.2a2 2 0 0 0 2 2h.4a2 2 0 0 0 2-2v-.2a2 2 0 0 1 1-1.7l.4-.2a2 2 0 0 1 2 0l.2.1a2 2 0 0 0 2.7-.7l.2-.4a2 2 0 0 0-.7-2.7l-.2-.1a2 2 0 0 1-1-1.7v-.5a2 2 0 0 1-1-1.7l-.2-.1a2 2 0 0 0 .7-2.7l-.2-.4a2 2 0 0 0-2.7-.7l-.2.1a2 2 0 0 1-2 0l-.4-.2a2 2 0 0 1-1-1.7l-.4-.2a2 2 0 0 1-1-1.7V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`),
	"pause":   glyph(`<rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/>`),
	"play":    glyph(`<path d="M6 3l14 9-14 9z"/>`),
	"stop":    glyph(`<rect x="6" y="6" width="12" height="12" rx="1"/>`),
	"refresh": glyph(`<path d="M3 12a9 9 0 0 1 15-6.7L21 8M21 3v5h-5M21 12a9 9 0 0 1-15 6.7L3 16M3 21v-5h5"/>`),
	"folder":  glyph(`<path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9l-.8-1.2A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2z"/>`),
	"copy":    glyph(`<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>`),
	"up":      glyph(`<path d="M12 19V5M5 12l7-7 7 7"/>`),
	"right":   glyph(`<path d="m9 18 6-6-6-6"/>`),
	"down":    glyph(`<path d="m6 9 6 6 6-6"/>`),
	"chup":    glyph(`<path d="m18 15-6-6-6 6"/>`),
	"x":       glyph(`<path d="M18 6 6 18M6 6l12 12"/>`),
	"export":  glyph(`<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/>`),
	"more":    glyph(`<circle cx="5" cy="12" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="19" cy="12" r="1.2"/>`),
	"info":    glyph(`<circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/>`),
	"search":  glyph(`<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>`),
	"hdd":     glyph(`<path d="M22 12H2M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11zM6 16h.01M10 16h.01"/>`),
	"pkg":     glyph(`<path d="M21 8a2 2 0 0 0-1-1.7l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.7l7 4a2 2 0 0 0 2 0l7-4a2 2 0 0 0 1-1.7z"/><path d="M3.3 7 12 12l8.7-5M12 22V12"/>`),
	"zap":     glyph(`<path d="M13 2 3 14h9l-1 8 10-12h-9z"/>`),
	"chart":   glyph(`<path d="M12 3a9 9 0 1 0 9 9h-9z"/><path d="M15 3.5A9 9 0 0 1 20.5 9H15z"/>`),
	"proc":    glyph(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M8 14h3"/>`),
	"cpu":     glyph(`<rect x="5" y="5" width="14" height="14" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"/>`),
	"mem":     glyph(`<rect x="2" y="7" width="20" height="10" rx="1.5"/><path d="M6 17v3M10 17v3M14 17v3M18 17v3M6 11v2M10 11v2M14 11v2M18 11v2"/>`),
	"dn":      glyph(`<path d="M12 4v14M6 12l6 6 6-6"/>`),
	"upa":     glyph(`<path d="M12 20V6M6 12l6-6 6 6"/>`),
}

func glyph(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

// icon creates a sized, tinted glyph element.
func icon(c *ui.Context, name string, size float32) ui.Element {
	return ui.Icon(c, glyphs[name]).FontSize(size).Shrink(0)
}
