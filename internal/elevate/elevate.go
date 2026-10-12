// Package elevate is the parent-side client of the elevated worker:
// DumpingGround runs unelevated; the first operation that hits
// ACCESS_DENIED spawns "DumpingGround.exe --elevated-worker" through the
// shell's runas verb — one UAC prompt, in context — and keeps the
// resulting helper alive for the rest of the session.
//
// The worker is this app's private, short-lived executor: it answers
// one named pipe, only from the parent PID that spawned it, and runs a
// closed set of operations — no shell, no arbitrary commands. The heavy
// features ride on handle donation (volume, process); only registry
// deletion, file deletion and protected-directory listing are remote
// ops.
package elevate

import "errors"

// Op is one verb of the worker protocol — a deliberately closed set.
type Op string

const (
	OpPing        Op = "ping"
	OpOpenVolume  Op = "open_volume"  // Drive "C:" → donated volume handle
	OpOpenProcess Op = "open_process" // PID + access mask → donated handle
	OpRegDelete   Op = "reg_delete"   // HKLM key tree or single value
	OpDeletePath  Op = "delete_path"  // Recycle or RemoveAll, worker-side
	OpListKids    Op = "list_kids"    // dir listing of a protected dir
	OpListTree    Op = "list_tree"    // recursive listing — Kid.Name is root-relative
	OpShutdown    Op = "shutdown"
)

// Request is the client → worker frame. Fields apply per Op.
type Request struct {
	Op      Op     `json:"op"`
	Path    string `json:"path,omitempty"`    // delete_path, list_kids
	Drive   string `json:"drive,omitempty"`   // open_volume — "C:" form
	PID     uint32 `json:"pid,omitempty"`     // open_process
	Access  uint32 `json:"access,omitempty"`  // open_process — desired access mask
	Key     string `json:"key,omitempty"`     // reg_delete — HKLM-relative path
	Value   string `json:"value,omitempty"`   // reg_delete — empty removes the key tree
	Reg32   bool   `json:"reg32,omitempty"`   // reg_delete — 32-bit (WOW6432) view
	Recycle bool   `json:"recycle,omitempty"` // delete_path
}

// Response is the worker → client frame.
type Response struct {
	OK bool `json:"ok"`
	// Err carries the Windows error text — the client maps
	// access-denied-ish text back onto ErrDenied when needed.
	Err    string `json:"err,omitempty"`
	Handle uint64 `json:"handle,omitempty"` // donated handle value, valid in the client process
	Kids   []Kid  `json:"kids,omitempty"`   // list_kids
}

// Kid is one directory entry from a protected listing; the three
// timestamps keep the age rules identical to a local walk, and Reparse
// keeps junction points out of every consumer's reach.
type Kid struct {
	Name    string `json:"name"` // leaf name; for OpListTree the root-relative path
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	ModUnix int64  `json:"mod"`
	CrtUnix int64  `json:"crt"`
	AccUnix int64  `json:"acc"`
	Reparse bool   `json:"reparse"`
}

var (
	// ErrUnsupported is what non-Windows builds return — callers treat it
	// as "no helper exists, keep the original error".
	ErrUnsupported = errors.New("elevated worker is only available on Windows")
	// ErrCancelled means the user declined the UAC prompt.
	ErrCancelled = errors.New("cancelled at the UAC prompt")
)
