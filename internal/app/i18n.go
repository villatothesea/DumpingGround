package app

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// Interface languages. The setting stores one of langOptions; "System"
// resolves through the OS UI language at startup.
const (
	langSystem = iota
	langEN
	langZhCN
	langZhTW
	langJA
	langKO
)

// langOptions are the Language select's choices, in order.
var langOptions = []string{"System", "English", "简体中文", "繁體中文", "日本語", "한국어"}

// langByName maps a settings value to its language id; unknown or
// legacy values (the earlier "中文" placeholder) fall back to System.
func langByName(name string) int {
	switch name {
	case "English":
		return langEN
	case "简体中文", "中文":
		return langZhCN
	case "繁體中文":
		return langZhTW
	case "日本語":
		return langJA
	case "한국어":
		return langKO
	}
	return langSystem
}

// curLang is the effective language — resolved at startup, switched by
// the Language setting. Views call tr() to render it.
var curLang atomic.Int32

// langOverride pins the effective language for tests regardless of the
// OS or stored setting; -1 means follow the settings.
var langOverride atomic.Int32

func init() { langOverride.Store(-1) }

// applyLanguage stores the effective id for tr(). "System" follows the
// OS UI language; anything without a table renders English, the source
// language every key is written in.
func (m *Model) applyLanguage() {
	if o := langOverride.Load(); o >= 0 {
		curLang.Store(o)
		return
	}
	id := langByName(m.language)
	if id == langSystem {
		id = systemLang()
	}
	curLang.Store(int32(id))
}

// trSelect renders a Select whose options display translated, while
// the bound value stays the canonical English option — stored settings
// never depend on the display language.
func trSelect(c *ui.Context, cur string, opts []string, set func(string)) ui.Element {
	disp := make([]string, len(opts))
	for i, o := range opts {
		disp[i] = tr(o)
	}
	sel := tr(cur)
	e := ui.Select(c, &sel, disp)
	if e.Changed() {
		if i := slices.Index(disp, sel); i >= 0 {
			set(opts[i])
		}
	}
	return e
}

// tr translates an English source string into the active language. The
// English literal is the key — a missing entry renders the key itself,
// so an untranslated string can never break a page.
func tr(s string) string {
	var tab map[string]string
	switch curLang.Load() {
	case int32(langZhCN):
		tab = langZhCNStrings
	case int32(langZhTW):
		tab = langZhTWStrings
	case int32(langJA):
		tab = langJAStrings
	case int32(langKO):
		tab = langKOStrings
	}
	if tab != nil {
		if t, ok := tab[s]; ok {
			return t
		}
	}
	return s
}

// trSkip renders the data layer's "X is running" guard strings in the
// active language; the process/app name stays a proper noun.
func trSkip(s string) string {
	if n, ok := strings.CutSuffix(s, " is running"); ok {
		return fmt.Sprintf(tr("%s is running"), n)
	}
	return tr(s)
}

// trWarn translates a Warn note: exact keys first, then the composed
// forms the disk layer builds — "Re-downloads on demand — X",
// "Crash reports queued by X — …" and friends.
func trWarn(s string) string {
	if t := tr(s); t != s {
		return t
	}
	if rest, ok := strings.CutPrefix(s, "Re-downloads on demand — "); ok {
		return tr("Re-downloads on demand — ") + tr(rest)
	}
	if rest, ok := strings.CutPrefix(s, "Crash reports queued by "); ok {
		if n, ok2 := strings.CutSuffix(rest, " — evidence of past crashes"); ok2 {
			return fmt.Sprintf(tr("Crash reports queued by %s — evidence of past crashes"), n)
		}
	}
	if n, ok := strings.CutSuffix(s, " re-caches on next launch — sign-ins and settings stay"); ok {
		return fmt.Sprintf(tr("%s re-caches on next launch — sign-ins and settings stay"), n)
	}
	if rest, ok := strings.CutPrefix(s, "Evidence of an "); ok {
		if n, ok2 := strings.CutSuffix(rest, " crash — keep it if you are still diagnosing"); ok2 {
			return fmt.Sprintf(tr("Evidence of an %s crash — keep it if you are still diagnosing"), n)
		}
	}
	return s
}

// trWhy renders the leftover scanner's reason strings. Most are fixed
// literals; four forms embed the app/object name — "installed-apps
// entry for "X" — its uninstaller is gone" and friends — and the
// history pass prefixes a name with ": ". The name never translates.
func trWhy(s string) string {
	if i := strings.Index(s, ": "); i > 0 {
		if t := trWhy(s[i+2:]); t != s[i+2:] {
			return s[:i+2] + t
		}
	}
	if rest, ok := strings.CutPrefix(s, `installed-apps entry for "`); ok {
		if n, ok2 := strings.CutSuffix(rest, `" — its uninstaller is gone`); ok2 {
			return fmt.Sprintf(tr(`installed-apps entry for "%s" — its uninstaller is gone`), n)
		}
	}
	if rest, ok := strings.CutPrefix(s, "startup entry "); ok {
		if n, ok2 := strings.CutSuffix(rest, " points to a missing file"); ok2 {
			return fmt.Sprintf(tr("startup entry %s points to a missing file"), n)
		}
	}
	if rest, ok := strings.CutPrefix(s, "file association for "); ok {
		if n, ok2 := strings.CutSuffix(rest, " runs a missing program"); ok2 {
			return fmt.Sprintf(tr("file association for %s runs a missing program"), n)
		}
	}
	return tr(s)
}

// trScanStage renders the footprint/orphan scan's stage labels —
// mostly fixed literals, with "Scanning <path>" carrying a live path.
func trScanStage(s string) string {
	if t := tr(s); t != s {
		return t
	}
	if rest, ok := strings.CutPrefix(s, "Scanning "); ok {
		return fmt.Sprintf(tr("Scanning %s"), rest)
	}
	return s
}

// trStartLocation renders a startup item's location — "Service <name>"
// and "Task Scheduler <key>" carry the store's name, the rest is a path.
func trStartLocation(s string) string {
	if rest, ok := strings.CutPrefix(s, "Service "); ok {
		return tr("Service ") + rest
	}
	if rest, ok := strings.CutPrefix(s, "Task Scheduler "); ok {
		return tr("Task Scheduler ") + rest
	}
	return s
}

// trHealthReason renders the drive-health verdict's observation lines.
// They arrive formatted ("N sectors were reallocated"), so each known
// shape is pattern-matched back into its format key; anything new
// passes through in English.
func trHealthReason(s string) string {
	if rest, ok := strings.CutPrefix(s, "critical warning: "); ok {
		return tr("critical warning: ") + tr(rest)
	}
	if rest, ok := strings.CutSuffix(s, " minutes spent above the critical temperature"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d minutes spent above the critical temperature"), v)
		}
	}
	if rest, ok := strings.CutSuffix(s, " minutes spent above the warning temperature"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d minutes spent above the warning temperature"), v)
		}
	}
	if rest, ok := strings.CutSuffix(s, " sectors are pending reallocation"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d sectors are pending reallocation"), v)
		}
	}
	if rest, ok := strings.CutSuffix(s, " sectors were reallocated"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d sectors were reallocated"), v)
		}
	}
	if rest, ok := strings.CutSuffix(s, " uncorrectable errors were reported"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d uncorrectable errors were reported"), v)
		}
	}
	if rest, ok := strings.CutPrefix(s, "available spare "); ok {
		var have, thr int
		if _, e := fmt.Sscanf(rest, "%d%% is at or below the drive's %d%% threshold", &have, &thr); e == nil {
			return fmt.Sprintf(tr("available spare %d%% is at or below the drive's %d%% threshold"), have, thr)
		}
	}
	if rest, ok := strings.CutSuffix(s, "% of the rated endurance is used"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d%% of the rated endurance is used"), v)
		}
	}
	if rest, ok := strings.CutPrefix(s, "the drive reports "); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d%% of its rated life left", &v); e == nil {
			return fmt.Sprintf(tr("the drive reports %d%% of its rated life left"), v)
		}
		if _, e := fmt.Sscanf(rest, "%d%% of its endurance remaining", &v); e == nil {
			return fmt.Sprintf(tr("the drive reports %d%% of its endurance remaining"), v)
		}
	}
	if rest, ok := strings.CutSuffix(s, " media/data-integrity error recorded"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d media/data-integrity %s recorded"), v, tr("errors"))
		}
	}
	if rest, ok := strings.CutSuffix(s, " media/data-integrity errors recorded"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d media/data-integrity %s recorded"), v, tr("errors"))
		}
	}
	if rest, ok := strings.CutSuffix(s, " offline uncorrectable sector"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d offline uncorrectable %s"), v, tr("sectors"))
		}
	}
	if rest, ok := strings.CutSuffix(s, " offline uncorrectable sectors"); ok {
		var v int
		if _, e := fmt.Sscanf(rest, "%d", &v); e == nil {
			return fmt.Sprintf(tr("%d offline uncorrectable %s"), v, tr("sectors"))
		}
	}
	var hex int
	if _, e := fmt.Sscanf(s, "critical warning 0x%02x", &hex); e == nil {
		return fmt.Sprintf(tr("critical warning 0x%02x"), hex)
	}
	return tr(s)
}
