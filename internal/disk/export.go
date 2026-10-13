package disk

import (
	"encoding/csv"
	"io"
	"strconv"
)

// WriteCSV exports the immutable scan result; no new filesystem reads occur.
func (s *Snapshot) WriteCSV(w io.Writer) error {
	status := s.scanStatuses()
	out := csv.NewWriter(w)
	if err := out.Write([]string{"Path", "Type", "Logical bytes", "Allocated bytes", "Shared hard link", "Scan status", "Error"}); err != nil {
		return err
	}
	stack := []int{s.Root}
	seen := map[int]bool{s.Root: true}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := s.Nodes[id]
		kind := "File"
		if n.Dir {
			kind = "Directory"
			for _, k := range n.Children {
				if !seen[k] {
					seen[k] = true
					stack = append(stack, k)
				}
			}
		}
		if err := out.Write([]string{s.FullPath(id), kind, strconv.FormatUint(n.Bytes, 10), strconv.FormatUint(n.Allocated, 10), strconv.FormatBool(n.Shared), status[id], s.ErrorAt(id)}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// scanStatuses labels every node: "unavailable" when the node itself
// could not be read, "partial" when it is fine but something beneath
// it failed, "complete" otherwise. One post-order pass so export does
// not re-walk subtrees per row.
func (s *Snapshot) scanStatuses() []string {
	st := make([]string, len(s.Nodes))
	type frame struct{ id, next int }
	stack := []frame{{id: s.Root}}
	done := make([]bool, len(s.Nodes))
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		if f.next < len(s.Nodes[f.id].Children) {
			k := s.Nodes[f.id].Children[f.next]
			f.next++
			if !done[k] {
				stack = append(stack, frame{id: k})
			}
			continue
		}
		v := "complete"
		if s.errs[f.id] != "" {
			v = "unavailable"
		} else {
			for _, k := range s.Nodes[f.id].Children {
				if st[k] != "" && st[k] != "complete" {
					v = "partial"
					break
				}
			}
		}
		st[f.id], done[f.id] = v, true
		stack = stack[:len(stack)-1]
	}
	return st
}
