package app

import "github.com/egoist/mygo/ui"

// Glyph paths come from the ui-redesign prototype (24×24, stroke icons).
var glyphs = map[string]*ui.SVG{
	"sun":     thinGlyph(`<path d="M14.828 14.828a4 4 0 1 0 -5.656 -5.656a4 4 0 0 0 5.656 5.656"/><path d="M6.343 17.657l-1.414 1.414"/><path d="M6.343 6.343l-1.414 -1.414"/><path d="M17.657 6.343l1.414 -1.414"/><path d="M17.657 17.657l1.414 1.414"/><path d="M4 12h-2"/><path d="M12 4v-2"/><path d="M20 12h2"/><path d="M12 20v2"/>`),
	"moon":    thinGlyph(`<path d="M12 3c.132 0 .263 0 .393 0a7.5 7.5 0 0 0 7.92 12.446a9 9 0 1 1 -8.313 -12.454l0 .008"/>`),
	"gear":    thinGlyph(`<path d="M19.875 6.27a2.225 2.225 0 0 1 1.125 1.948v7.284c0 .809 -.443 1.555 -1.158 1.948l-6.75 4.27a2.269 2.269 0 0 1 -2.184 0l-6.75 -4.27a2.225 2.225 0 0 1 -1.158 -1.948v-7.285c0 -.809 .443 -1.554 1.158 -1.947l6.75 -3.98a2.33 2.33 0 0 1 2.25 0l6.75 3.98h-.033"/><path d="M9 12a3 3 0 1 0 6 0a3 3 0 1 0 -6 0"/>`),
	"pause":   glyph(`<rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/>`),
	"play":    glyph(`<path d="M6 3l14 9-14 9z"/>`),
	"stop":    glyph(`<rect x="6" y="6" width="12" height="12" rx="1"/>`),
	"refresh": glyph(`<path d="M3 12a9 9 0 0 1 15-6.7L21 8M21 3v5h-5M21 12a9 9 0 0 1-15 6.7L3 16M3 21v-5h5"/>`),
	"folder":  glyph(`<path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9l-.8-1.2A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2z"/>`),
	"file":    glyph(`<path d="M14 3v4a1 1 0 0 0 1 1h4"/><path d="M17 21H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h7l5 5v11a2 2 0 0 1-2 2z"/>`),
	"copy":    glyph(`<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>`),
	"up":      glyph(`<path d="M12 19V5M5 12l7-7 7 7"/>`),
	"right":   glyph(`<path d="m9 18 6-6-6-6"/>`),
	"down":    glyph(`<path d="m6 9 6 6 6-6"/>`),
	"chup":    glyph(`<path d="m18 15-6-6-6 6"/>`),
	"x":       glyph(`<path d="M18 6 6 18M6 6l12 12"/>`),
	"trash":   glyph(`<path d="M4 7l16 0"/><path d="M10 11l0 6"/><path d="M14 11l0 6"/><path d="M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2 -2l1 -12"/><path d="M9 7v-3a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v3"/>`),
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
	// Logo option 10 of the prototype: a flag planted on a junk hill.
	"logo": glyph(`<path d="M2.4 20.6c1.6-5.4 4.6-8.6 8.2-8.6s6.6 3.2 8.2 8.6z"/><path d="M2 20.6h20"/><path d="M10.8 12V3.2"/><path d="M10.8 3.2l6 2.2-6 2.4"/><path d="M7.8 17.2h.01M12.4 16.4h.01M14.4 18.6h.01"/>`),
	"rust":    glyph(`<path d="M10.139 3.463c.473 -1.95 3.249 -1.95 3.722 0a1.916 1.916 0 0 0 2.859 1.185c1.714 -1.045 3.678 .918 2.633 2.633a1.916 1.916 0 0 0 1.184 2.858c1.95 .473 1.95 3.249 0 3.722a1.916 1.916 0 0 0 -1.185 2.859c1.045 1.714 -.918 3.678 -2.633 2.633a1.916 1.916 0 0 0 -2.858 1.184c-.473 1.95 -3.249 1.95 -3.722 0a1.916 1.916 0 0 0 -2.859 -1.185c-1.714 1.045 -3.678 -.918 -2.633 -2.633a1.916 1.916 0 0 0 -1.184 -2.858c-1.95 -.473 -1.95 -3.249 0 -3.722a1.916 1.916 0 0 0 1.185 -2.859c-1.045 -1.714 .918 -3.678 2.633 -2.633a1.914 1.914 0 0 0 2.858 -1.184"/><path d="M8 12h6a2 2 0 1 0 0 -4h-6v8v-4"/><path d="M19 16h-2a2 2 0 0 1 -2 -2a2 2 0 0 0 -2 -2h-1"/><path d="M9 8h-4"/><path d="M5 16h4"/>`),
	"broom": glyph(`<path d="M17.5 3.5l-8.5 8.5"/><path d="M9 12l3.5 3.5"/><path d="M11.5 12.5l-7.5 7.5"/><path d="M13.5 14.5l-6 6"/><path d="M15.5 16.5l-3.5 3.5"/><path d="M5.5 18.5l-1.5 1.5"/>`),
	"caret-r": glyph(`<path d="M9 5l7 7-7 7z" fill="currentColor" stroke="none"/>`),
	"caret-d": glyph(`<path d="M5 9h14l-7 7z" fill="currentColor" stroke="none"/>`),
	"warn":    glyph(`<path d="M12 3l10 18H2z"/><path d="M12 10v5"/><path d="M12 18.5h.01"/>`),
	"net":  glyph(`<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>`),
	"end":  glyph(`<path d="M18.4 6.6a8 8 0 1 0 0 10.8"/><path d="M12 8v4"/>`),
}

func glyph(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

// thinGlyph draws at the Tabler icons' 1px stroke instead of the set's 2px.
func thinGlyph(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

// icon creates a sized, tinted glyph element.
func icon(c *ui.Context, name string, size float32) ui.Element {
	return ui.Icon(c, glyphs[name]).FontSize(size).Shrink(0)
}
