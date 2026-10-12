// Package startup lists everything that starts with Windows or with the
// user's session — Run keys, Startup folders, automatic services and
// logon/boot scheduled tasks — and switches items on and off without
// deleting anything.
//
// Every switch is reversible by construction: apps are toggled through the
// StartupApproved keys (the mechanism Task Manager itself uses, the Run
// value stays), tasks through Task Scheduler's own enable flag, services
// through their start type with the original remembered. Parts of Windows
// are listed for transparency but locked.
package startup

import "errors"

// Kind is where an item comes from.
type Kind uint8

const (
	KindRun     Kind = iota // a value under a Run key
	KindFolder              // a file in a Startup folder
	KindService             // an automatic Windows service
	KindTask                // a scheduled task that fires at logon or boot
)

func (k Kind) String() string {
	return [...]string{"Startup app", "Startup folder", "Service", "Scheduled task"}[k]
}

// Item is one startup entry.
type Item struct {
	Kind      Kind
	Key       string // unique and stable: where the item lives
	Name      string
	Command   string // the command line as configured
	Exe       string // the executable it resolves to, when known
	Publisher string
	Desc      string
	Location  string // where it is configured, for display
	Scope     string // "Current user", "All users", "System"
	Trigger   string // "At sign-in", "At startup", "Automatic", "Automatic (delayed)"
	Enabled   bool
	Running   bool   // the program or service is running right now
	Windows   bool   // a component of Windows — hidden by default
	Locked    string // non-empty: why the switch is disabled

	// Where the switch lives, for SetEnabled.
	reg      string // full registry path of the item, for "Open in Registry Editor"
	hive     string // "HKCU" | "HKLM"
	approved string // StartupApproved subkey: "Run", "Run32", "StartupFolder"
	value    string // value name under the approved key
	start    uint32 // services: the Start type as read
	delayed  bool   // services: delayed auto-start
}

// App reports whether the item is software launched with the session.
func (it *Item) App() bool { return it.Kind == KindRun || it.Kind == KindFolder }

// ErrUnsupported is what non-Windows builds return.
var ErrUnsupported = errors.New("startup management is only available on Windows")
