package app

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dumpingground/dumpingground/internal/startup"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// startupList and startupSet are the page's only contact with the system;
// tests replace them so no test ever changes a real startup item.
var (
	startupList = startup.List
	startupSet  = startup.SetEnabled
)

// startupState is the Startup page's model.
type startupState struct {
	items       []startup.Item
	loaded      bool // the list has been read at least once
	loading     bool
	loadedAt    time.Time
	filter      string // "All", "Apps", "Services" or "Tasks"; "" is All
	showWindows bool   // list Windows' own services and tasks (read-only)
	query       string
	sort        ui.SortOrder
	list        ui.ListState
	selected    int    // row index the table highlights
	selKey      string // the selected item's key — survives re-sorts and reloads
	menuHits    []menuHit
	confirm     bool // the "turn off this service?" dialog is open
	confirmItem startup.Item
	busy        string // key of the item being switched
	msg         string
}

var startupFilters = []string{"All", "Apps", "Services", "Tasks"}

// reloadStartup re-reads every source off the UI thread, keeping the old
// list on screen until the new one lands.
func (m *Model) reloadStartup() {
	st := &m.st
	if st.loading {
		return
	}
	st.loading = true
	go func() {
		items := startupList()
		m.post(func() {
			st.items, st.loaded, st.loading, st.loadedAt = items, true, false, time.Now()
		})
	}()
}

// startupRows applies the search box, the Windows toggle and the source
// filter, then the column sort. counts are the sizes of All, Apps,
// Services and Tasks before the source filter; hidden is how many Windows
// components the toggle is keeping out.
func (m *Model) startupRows() (rows []*startup.Item, counts [4]int, hidden int) {
	st := &m.st
	q := strings.ToLower(strings.TrimSpace(st.query))
	for i := range st.items {
		it := &st.items[i]
		if it.Windows && !st.showWindows {
			hidden++
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.Name+"\n"+it.Publisher+"\n"+it.Command+"\n"+it.Desc), q) {
			continue
		}
		counts[0]++
		switch it.Kind {
		case startup.KindRun, startup.KindFolder:
			counts[1]++
		case startup.KindService:
			counts[2]++
		case startup.KindTask:
			counts[3]++
		}
		if st.filter == "Apps" && !it.App() || st.filter == "Services" && it.Kind != startup.KindService || st.filter == "Tasks" && it.Kind != startup.KindTask {
			continue
		}
		rows = append(rows, it)
	}
	slices.SortStableFunc(rows, func(a, b *startup.Item) int {
		var c int
		switch st.sort.Column {
		case "Name":
			c = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "Publisher":
			c = cmp.Compare(strings.ToLower(a.Publisher), strings.ToLower(b.Publisher))
		case "Status":
			c = cmp.Compare(startupStatusRank(a), startupStatusRank(b))
		case "Command":
			c = cmp.Compare(strings.ToLower(a.Command), strings.ToLower(b.Command))
		case "Starts":
			c = cmp.Compare(startupStartsLabel(a), startupStartsLabel(b))
		default: // "Type": apps, services, tasks — switched-on items first within each
			c = cmp.Or(cmp.Compare(startupKindRank(a), startupKindRank(b)), cmp.Compare(b2i(!a.Enabled), b2i(!b.Enabled)))
		}
		if c == 0 {
			c = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		} else if st.sort.Descending {
			c = -c
		}
		return c
	})
	return rows, counts, hidden
}

func startupKindRank(it *startup.Item) int {
	switch it.Kind {
	case startup.KindRun:
		return 0
	case startup.KindFolder:
		return 1
	case startup.KindService:
		return 2
	}
	return 3
}

func startupStatusRank(it *startup.Item) int {
	switch {
	case it.Running && it.Enabled:
		return 0
	case it.Enabled:
		return 1
	case it.Running:
		return 2
	}
	return 3
}

// startupStartsLabel is the Starts column: for apps, which mechanism and
// whose; for services and tasks, when they fire.
func startupStartsLabel(it *startup.Item) string {
	switch it.Kind {
	case startup.KindService, startup.KindTask:
		return it.Trigger
	case startup.KindFolder:
		return "Startup folder · " + it.Scope
	}
	return "Run key · " + it.Scope
}

// startupCategory is the Category column: what kind of thing this is.
// Run keys and Startup folders are both apps - the Starts column tells
// them apart.
func startupCategory(it *startup.Item) string {
	switch it.Kind {
	case startup.KindService:
		return "Service"
	case startup.KindTask:
		return "Task"
	}
	return "App"
}

func (m *Model) startupView(c *ui.Context, t *ui.Theme) {
	st := &m.st
	if !st.loaded && !st.loading {
		m.reloadStartup()
	}
	if st.sort.Column == "" {
		st.sort = ui.SortOrder{Column: "Category"}
		st.list.Sort = &st.sort
	}
	rows, counts, hidden := m.startupRows()
	ui.Column(c).Fill().Padding(4, 12).Gap(8).Children(func() {
		m.startupToolbar(c, t, rows, counts, hidden)
		switch {
		case !st.loaded:
			ui.Text(c, "Reading startup items…").TextColor(t.TextMuted)
		case len(rows) == 0:
			msg := "Nothing starts from here."
			if st.query != "" {
				msg = "No startup item matches this filter."
			}
			ui.Text(c, msg).TextColor(t.TextMuted)
		default:
			m.startupTable(c, t, rows)
		}
		m.startupDetail(c, t, rows)
	})
	m.startupConfirm(c)
}

func (m *Model) startupToolbar(c *ui.Context, t *ui.Theme, rows []*startup.Item, counts [4]int, hidden int) {
	st := &m.st
	ui.Row(c).FillWidth().Height(30).AlignItems(ui.Center).Gap(8).Children(func() {
		icon(c, "zap", 18)
		on, off := 0, 0
		for _, r := range rows {
			if r.Enabled {
				on++
			} else {
				off++
			}
		}
		head := "Startup"
		if st.loaded {
			head = fmt.Sprintf("Startup — %d on · %d off", on, off)
		}
		ui.Text(c, head).Bold().Shrink(0)
		if st.loading {
			ui.Text(c, "reading…").FontSize(11).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		sel := slices.Index(startupFilters, cmp.Or(st.filter, "All"))
		labels := make([]string, len(startupFilters))
		for i, f := range startupFilters {
			labels[i] = fmt.Sprintf("%s %d", f, counts[i])
		}
		m.segPair(c, t, "Startup source", labels, max(sel, 0), func(i int) { st.filter = startupFilters[i] })
		label := "Windows components"
		if hidden > 0 && !st.showWindows {
			label = fmt.Sprintf("Windows components (+%d)", hidden)
		}
		ui.Checkbox(c, &st.showWindows, label).Tooltip("Windows' own services and tasks. They are listed read-only — Windows manages them.")
		if m.iconButton(c, "refresh", "Re-read startup items (F5)").Clicked() {
			m.reloadStartup()
		}
	})
}

func (m *Model) startupTable(c *ui.Context, t *ui.Theme, rows []*startup.Item) {
	st := &m.st
	st.menuHits = st.menuHits[:0]
	prev := slices.IndexFunc(rows, func(r *startup.Item) bool { return r.Key == st.selKey })
	st.selected = prev
	st.list.Selected = &st.selected
	cols := []ui.TableColumn{
		{Title: "", ID: "switch", Width: 64, Fixed: true},
		{Title: "Name", Sortable: true, MinWidth: 160},
		{Title: "Category", Width: 86, MinWidth: 60, Sortable: true},
		{Title: "Publisher", Width: 190, MinWidth: 60, Sortable: true},
		{Title: "Starts", Width: 170, MinWidth: 60, Sortable: true},
		{Title: "Status", Width: 150, MinWidth: 60, Sortable: true},
		{Title: "Command", MinWidth: 220, Sortable: true},
	}
	tbl := tableChrome(c, func() ui.Element {
		return ui.Table(c, &st.list, cols, len(rows), func(row, col int) {
			it := rows[row]
			sel := row == st.selected
			text, muted := t.Text, t.TextMuted
			if sel {
				text, muted = t.AccentText, t.AccentText
			} else if !it.Enabled {
				text = t.TextMuted
			}
			cell := ui.Row(c).Fill().Height(30).Gap(6).AlignItems(ui.Center)
			if col == 0 {
				if b := cell.Bounds(); b.H > 0 {
					st.menuHits = append(st.menuHits, menuHit{y1: b.Y, y2: b.Y + b.H, id: row})
				}
			}
			cell.Children(func() {
				switch col {
				case 0:
					m.startupSwitch(c, t, it)
				case 1:
					if bmp := m.processIcon(it.Exe); bmp != nil && it.Exe != "" {
						ui.Image(c, bmp).Size(16, 16).Shrink(0)
					} else {
						icon(c, startupGlyph(it), 14).TextColor(muted)
					}
					ui.Text(c, it.Name).SingleLine().TextColor(text).Tooltip(it.Name)
					if it.Locked != "" {
						icon(c, "lock", 11).TextColor(muted).Tooltip(it.Locked)
					}
				case 2:
					ui.Text(c, startupCategory(it)).TextColor(muted).SingleLine()
				case 3:
					ui.Text(c, it.Publisher).TextColor(muted).SingleLine().Tooltip(it.Publisher)
				case 4:
					ui.Text(c, startupStartsLabel(it)).TextColor(muted).SingleLine().Tooltip(it.Location)
				case 5:
					m.startupStatus(c, t, it, muted)
				default:
					ui.Text(c, it.Command).TextColor(muted).SingleLine().Tooltip(it.Command)
				}
			})
		}).Grow(1).MinHeight(0).FillWidth()
	}).Grow(1).MinHeight(0).FillWidth()
	if st.selected != prev && st.selected >= 0 && st.selected < len(rows) {
		st.selKey = rows[st.selected].Key
	}
	tableRowMenu(tbl, &st.menuHits, func(id int, menu *ui.Menu) {
		st.selected, st.selKey = id, rows[id].Key
		m.startupMenu(c, menu, rows[id])
	})
}

// startupSwitch is a row's on/off switch. It is drawn from the item's real
// state every frame — a switch that was clicked but whose change failed, or
// is waiting for a confirmation, simply snaps back.
func (m *Model) startupSwitch(c *ui.Context, t *ui.Theme, it *startup.Item) {
	on := it.Enabled
	sw := ui.Switch(c, &on).Label(it.Name)
	switch {
	case it.Locked != "":
		sw = sw.Disabled(true).Tooltip(it.Locked)
	case m.st.busy != "":
		sw = sw.Disabled(true)
	case it.Enabled:
		sw = sw.Tooltip("Starts with Windows — switch off to stop it")
	default:
		sw = sw.Tooltip("Does not start — switch on to start it again")
	}
	if sw.Changed() {
		m.startupToggle(*it, on)
	}
}

func (m *Model) startupStatus(c *ui.Context, t *ui.Theme, it *startup.Item, muted ui.Color) {
	switch {
	case it.Running && !it.Enabled:
		ui.Text(c, "Running until restart").TextColor(t.Warning).SingleLine()
	case it.Running:
		ui.Box(c).Size(7, 7).Radius(4).Background(t.Success).Shrink(0)
		ui.Text(c, "Running").TextColor(muted).SingleLine()
	case !it.Enabled:
		ui.Text(c, "Off").TextColor(muted).SingleLine()
	default:
		ui.Text(c, "Not running").TextColor(muted).SingleLine()
	}
}

func startupGlyph(it *startup.Item) string {
	switch it.Kind {
	case startup.KindService:
		return "gear"
	case startup.KindTask:
		return "zap"
	}
	return "proc"
}

// startupToggle starts a switch. Turning a service off asks first — it can
// be something other software depends on — everything else is reversible
// enough to just do.
func (m *Model) startupToggle(it startup.Item, on bool) {
	if it.Kind == startup.KindService && !on {
		m.st.confirm, m.st.confirmItem = true, it
		return
	}
	m.applyStartup(it, on)
}

func (m *Model) applyStartup(it startup.Item, on bool) {
	st := &m.st
	st.busy = it.Key
	go func() {
		err := startupSet(it, on)
		m.post(func() {
			st.busy = ""
			switch {
			case err != nil:
				st.msg = fmt.Sprintf("Could not switch %q: %v", it.Name, err)
			case on:
				st.msg = fmt.Sprintf("%q will start with Windows again", it.Name)
			case it.Kind == startup.KindService:
				st.msg = fmt.Sprintf("%q will not start at the next boot — it keeps running until then", it.Name)
			default:
				st.msg = fmt.Sprintf("%q will not start at the next sign-in", it.Name)
			}
			m.reloadStartup()
		})
	}()
}

// startupConfirm is the dialog behind turning a service off.
func (m *Model) startupConfirm(c *ui.Context) {
	st := &m.st
	if !st.confirm {
		return
	}
	it := st.confirmItem
	msg := fmt.Sprintf("%s (%s) will be set to Disabled and will not start at the next boot. Programs that depend on it may stop working. The current setting is remembered, so switching it back on restores it exactly.", it.Name, strings.TrimPrefix(it.Location, "Service "))
	if choice := ui.AlertDialog(c, &st.confirm, "Turn off this service?", msg, "Cancel", "Turn off"); choice == 1 {
		m.applyStartup(it, false)
	}
}

// startupDetail is the strip under the table: where the selected item is
// configured, what it runs, and what switching it does.
func (m *Model) startupDetail(c *ui.Context, t *ui.Theme, rows []*startup.Item) {
	st := &m.st
	var it *startup.Item
	for _, r := range rows {
		if r.Key == st.selKey {
			it = r
		}
	}
	panel := ui.Column(c).FillWidth().Height(118).Shrink(0).Padding(10, 14).Gap(3).Radius(8).Background(t.Surface)
	panel.BorderWidth(1).BorderColor(t.Border)
	panel.Children(func() {
		if it == nil {
			msg := "Select an item to see where it starts from and what switching it does."
			if st.msg != "" {
				msg = st.msg
			}
			ui.Text(c, msg).TextColor(t.TextMuted)
			return
		}
		who := it.Publisher
		if it.Desc != "" && it.Desc != it.Name {
			who = strings.TrimSpace(who + " · " + it.Desc)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, it.Name).Bold().SingleLine().Shrink(0)
			ui.Text(c, who).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…").MinWidth(0)
		})
		m.startupKV(c, t, "Starts from", it.Location+"  ·  "+it.Scope)
		m.startupKV(c, t, "Runs", it.Command)
		note := startupConsequence(it)
		ui.Text(c, note).FontSize(11).TextColor(t.TextMuted).MaxLines(2)
		if st.msg != "" {
			ui.Text(c, st.msg).FontSize(11).TextColor(t.Accent).SingleLine()
		}
	})
}

func (m *Model) startupKV(c *ui.Context, t *ui.Theme, k, v string) {
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, k).FontSize(11).TextColor(t.TextMuted).Width(72).Shrink(0)
		ui.Text(c, v).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0).Tooltip(v).Selectable()
	})
}

// startupConsequence says, in the item's own terms, what its switch does
// and that nothing is deleted.
func startupConsequence(it *startup.Item) string {
	switch {
	case it.Locked != "":
		return it.Locked + ". Use Services or Task Scheduler if you really need to change it."
	case it.Kind == startup.KindService && it.Enabled:
		return "Turning it off sets the service to Disabled for the next boot. Anything that depends on it may stop working. The original setting is remembered and restored when you switch it back on."
	case it.Kind == startup.KindService:
		return "Disabled. Switching it on restores the start type it had before (automatic, possibly delayed)."
	case it.Kind == startup.KindTask && it.Enabled:
		return "Turning it off disables the scheduled task. The task and its settings stay in Task Scheduler."
	case it.Kind == startup.KindTask:
		return "The task is disabled. Switching it on enables it in Task Scheduler again."
	case it.Enabled:
		return "Turning it off stops it from starting at sign-in. Nothing is deleted — the entry stays where it is and you can switch it back on."
	}
	return "Switched off — the entry is still there, Windows just skips it at sign-in."
}

func (m *Model) startupMenu(c *ui.Context, menu *ui.Menu, it *startup.Item) {
	label := "Turn off"
	if !it.Enabled {
		label = "Turn on"
	}
	if menu.Item(label).Disabled(it.Locked != "" || m.st.busy != "").Chosen() {
		m.startupToggle(*it, !it.Enabled)
	}
	menu.Separator()
	where := it.Exe
	if it.Kind == startup.KindFolder {
		where = it.Location
	}
	if menu.Item("Open file location").Disabled(where == "" || m.window == nil).Chosen() {
		mygo.Shell.ShowItemInFolder(filepath.Clean(where))
	}
	if menu.Item("Copy command").Chosen() {
		c.WriteClipboard(it.Command)
	}
	if menu.Item("Copy details").Chosen() {
		c.WriteClipboard(fmt.Sprintf("%s — %s\n%s · %s\nStarts from: %s (%s)\nRuns: %s", it.Name, it.Publisher, startupCategory(it), startupStartsLabel(it), it.Location, it.Scope, it.Command))
	}
	menu.Separator()
	if menu.Item("Open in Registry Editor").Disabled(it.Kind != startup.KindRun && it.Kind != startup.KindService).Chosen() {
		if err := startup.OpenInRegedit(*it); err != nil {
			m.st.msg = "regedit: " + err.Error()
		}
	}
}
