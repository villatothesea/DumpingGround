package disk

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"sync/atomic"
	"time"
)

type Node struct {
	ID, Parent       int
	Name             string
	Dir              bool
	Bytes, Allocated uint64
	Children         []int
	Shared           bool
	FRN              uint64 // NTFS file reference number: record | sequence<<48.
}
type Snapshot struct {
	Root                                              int
	Nodes                                             []Node
	Files, Dirs, Bytes, Allocated, Skipped, Estimated uint64
	// Reparse counts junctions and links skipped by design; Offline counts
	// cloud placeholders whose data is not on the disk. Unlinked counts
	// names dropped because the parent record was missing or carried a
	// stale sequence number — only NTFS MFT scans produce those.
	Reparse, Offline, Unlinked uint64
	Duration                   time.Duration
	Method, Fallback           string
	// JournalID and JournalUSN capture the USN change journal's position at
	// scan start on Windows; WatchJournal can replay everything since.
	// Zero for directory walks.
	JournalID  uint64
	JournalUSN int64
	// frns maps each file reference number to its first node; extra links of
	// hard-linked records live in frnMore, so a lone link costs one map slot.
	frns    map[uint64]int
	frnMore map[uint64][]int
	// errs holds per-node read errors. Almost every node has none, so they
	// live in a side map instead of costing a string field per node.
	errs map[int]string
}

// ErrorAt returns the read error recorded for a node, "" when it is clean.
func (s *Snapshot) ErrorAt(id int) string { return s.errs[id] }

func (s *Snapshot) setErr(id int, err string) {
	if s.errs == nil {
		s.errs = map[int]string{}
	}
	s.errs[id] = err
}

// maxPathDepth bounds parent-chain walks; a corrupt snapshot must degrade
// a path, never hang the caller.
const maxPathDepth = 256

func (s *Snapshot) FullPath(id int) string {
	if id < 0 || id >= len(s.Nodes) {
		return ""
	}
	n := s.Nodes[id]
	if n.Parent < 0 || n.Parent == id {
		return n.Name
	}
	return s.fullPath(id, 0)
}

func (s *Snapshot) fullPath(id, depth int) string {
	n := s.Nodes[id]
	if depth >= maxPathDepth || n.Parent < 0 || n.Parent == id {
		return n.Name
	}
	return filepath.Join(s.fullPath(n.Parent, depth+1), n.Name)
}

type Progress struct {
	Files, Dirs, Skipped, Bytes, Allocated, Estimated uint64
	Current                                           string
	Elapsed                                           time.Duration
	Method                                            string
}
type Scanner struct{ active atomic.Bool }

func (s *Scanner) Active() bool { return s.active.Load() }

// Scan owns its result until completion. Progress is lossy and rate limited;
// consumers must use the returned snapshot as the authoritative final state.
// Cancellation returns a partial snapshot as well as context.Canceled.
func (s *Scanner) Scan(ctx context.Context, root string, progress chan<- Progress) (*Snapshot, error) {
	// The index allocates on the order of a million nodes; running the
	// collector through that churn only slows the scan. Pause it and pay
	// one collection when the snapshot settles.
	prevGC := debug.SetGCPercent(-1)
	defer func() {
		debug.SetGCPercent(prevGC)
		runtime.GC()
	}()
	if len(root) == 3 && root[1:] == ":\\" {
		fast, fastErr := fastVolumeScan(ctx, root, progress)
		if fastErr == nil || errors.Is(fastErr, context.Canceled) || errors.Is(fastErr, context.DeadlineExceeded) {
			if fast != nil {
				// The FRN reverse map takes a second or more on a
				// million-node index; building it here keeps that work off
				// the UI goroutine, which would otherwise pay it lazily on
				// the first USN event and stall.
				fast.frnInit()
			}
			return fast, fastErr
		}
		fallback, walkErr := s.scanDirectory(ctx, root, progress)
		if fallback != nil {
			fallback.Fallback = fastErr.Error()
		}
		return fallback, walkErr
	}
	return s.scanDirectory(ctx, root, progress)
}

func (s *Scanner) scanDirectory(ctx context.Context, root string, progress chan<- Progress) (*Snapshot, error) {
	if !s.active.CompareAndSwap(false, true) {
		return nil, errors.New("a disk scan is already running")
	}
	defer s.active.Store(false)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("scan target is not a directory")
	}
	start := time.Now()
	out := &Snapshot{Method: "Directory walk", Nodes: []Node{{ID: 0, Parent: -1, Name: root, Dir: true}}}
	pending := []int{0}
	identities := make(map[string]struct{})
	last := time.Time{}
	report := func(path string, force bool) {
		if progress == nil || (!force && time.Since(last) < 100*time.Millisecond) {
			return
		}
		last = time.Now()
		p := Progress{Files: out.Files, Dirs: out.Dirs, Skipped: out.Skipped, Bytes: out.Bytes, Allocated: out.Allocated, Estimated: out.Estimated, Current: path, Elapsed: time.Since(start), Method: out.Method}
		select {
		case progress <- p:
		default:
		}
	}
	report(root, true)
	scanErr := error(nil)
	for len(pending) > 0 {
		if scanErr = ctx.Err(); scanErr != nil {
			break
		}
		parent := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		path := out.fullPath(parent, 0)
		dir, e := os.Open(path)
		if e != nil {
			if parent == 0 {
				return nil, e
			}
			out.setErr(parent, e.Error())
			out.Skipped++
			continue
		}
		for {
			if scanErr = ctx.Err(); scanErr != nil {
				break
			}
			entries, readErr := dir.ReadDir(256)
			for _, entry := range entries {
				if scanErr = ctx.Err(); scanErr != nil {
					break
				}
				full := filepath.Join(path, entry.Name())
				meta, e := entry.Info()
				if e != nil {
					out.Skipped++
					continue
				}
				// Never traverse reparse points or hydrate cloud placeholders.
				if k := skipAttrs(meta); k != 0 {
					out.Skipped++
					if k&0x400 != 0 {
						out.Reparse++
					} else {
						out.Offline++
					}
					continue
				}
				n := Node{ID: len(out.Nodes), Parent: parent, Name: entry.Name(), Dir: meta.IsDir()}
				if n.Dir {
					out.Dirs++
					pending = append(pending, n.ID)
				} else if meta.Mode().IsRegular() {
					n.Bytes = uint64(max(meta.Size(), 0))
					var known bool
					var identity string
					n.Allocated, known, identity = measureSpace(full, meta.Size())
					if identity != "" {
						if _, exists := identities[identity]; exists {
							n.Allocated = 0
							n.Shared = true
						} else {
							identities[identity] = struct{}{}
						}
					}
					if !known {
						out.Estimated++
					}
					out.Files++
					out.Bytes += n.Bytes
					out.Allocated += n.Allocated
				} else {
					out.Skipped++
					continue
				}
				out.Nodes[parent].Children = append(out.Nodes[parent].Children, n.ID)
				out.Nodes = append(out.Nodes, n)
				report(full, false)
			}
			if scanErr != nil {
				break
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					out.setErr(parent, readErr.Error())
					out.Skipped++
				}
				break
			}
		}
		dir.Close()
		if scanErr != nil {
			break
		}
	}
	// Parents are always inserted first, so reverse order is a post-order
	// aggregation without recursion, even for very deeply nested directories.
	for i := len(out.Nodes) - 1; i > 0; i-- {
		n := &out.Nodes[i]
		p := &out.Nodes[n.Parent]
		p.Bytes += n.Bytes
		p.Allocated += n.Allocated
	}
	for i := range out.Nodes {
		kids := out.Nodes[i].Children
		sort.Slice(kids, func(a, b int) bool {
			x, y := out.Nodes[kids[a]], out.Nodes[kids[b]]
			if x.Allocated == y.Allocated {
				return x.Name < y.Name
			}
			return x.Allocated > y.Allocated
		})
	}
	out.Duration = time.Since(start)
	report(root, true)
	return out, scanErr
}
