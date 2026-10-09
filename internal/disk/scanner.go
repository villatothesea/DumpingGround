package disk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
)

type Node struct {
	ID       int
	Parent   int
	Name     string
	Path     string
	Dir      bool
	Bytes    uint64
	Children []int
	Error    string
}

type Snapshot struct {
	Root    int
	Nodes   []Node
	Files   uint64
	Dirs    uint64
	Bytes   uint64
	Skipped uint64
}

type Progress struct {
	Files, Dirs, Skipped, Bytes uint64
	Current                     string
}

type Scanner struct {
	active atomic.Bool
}

func (s *Scanner) Active() bool { return s.active.Load() }

func (s *Scanner) Scan(ctx context.Context, root string, progress chan<- Progress) (*Snapshot, error) {
	if !s.active.CompareAndSwap(false, true) {
		return nil, errors.New("a disk scan is already running")
	}
	defer s.active.Store(false)
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
	out := &Snapshot{Root: 0, Nodes: []Node{{ID: 0, Parent: -1, Name: filepath.Base(root), Path: root, Dir: true}}}
	var walk func(int) error
	walk = func(parent int) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		entries, err := os.ReadDir(out.Nodes[parent].Path)
		if err != nil {
			out.Nodes[parent].Error = err.Error()
			out.Skipped++
			return nil
		}
		for _, entry := range entries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			path := filepath.Join(out.Nodes[parent].Path, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				out.Skipped++
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				out.Skipped++
				out.Nodes[parent].Children = append(out.Nodes[parent].Children, len(out.Nodes))
				out.Nodes = append(out.Nodes, Node{ID: len(out.Nodes), Parent: parent, Name: entry.Name(), Path: path, Error: infoErr.Error()})
				continue
			}
			n := Node{ID: len(out.Nodes), Parent: parent, Name: entry.Name(), Path: path, Dir: info.IsDir()}
			if !n.Dir {
				n.Bytes = uint64(max(info.Size(), 0))
				out.Files++
				out.Bytes += n.Bytes
			} else {
				out.Dirs++
			}
			out.Nodes[parent].Children = append(out.Nodes[parent].Children, n.ID)
			out.Nodes = append(out.Nodes, n)
			if progress != nil {
				select {
				case progress <- Progress{Files: out.Files, Dirs: out.Dirs, Skipped: out.Skipped, Bytes: out.Bytes, Current: path}:
				default:
				}
			}
			if n.Dir {
				if err := walk(n.ID); err != nil {
					return err
				}
			}
		}
		var total uint64
		for _, child := range out.Nodes[parent].Children {
			total += out.Nodes[child].Bytes
		}
		out.Nodes[parent].Bytes = total
		sort.SliceStable(out.Nodes[parent].Children, func(i, j int) bool {
			return out.Nodes[out.Nodes[parent].Children[i]].Bytes > out.Nodes[out.Nodes[parent].Children[j]].Bytes
		})
		return nil
	}
	if err := walk(0); err != nil {
		return nil, err
	}
	return out, nil
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
