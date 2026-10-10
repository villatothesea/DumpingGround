package disk

import (
	"encoding/csv"
	"io"
	"strconv"
)

// WriteCSV exports the immutable scan result; no new filesystem reads occur.
func (s *Snapshot) WriteCSV(w io.Writer) error {
	out := csv.NewWriter(w)
	if err := out.Write([]string{"Path", "Type", "Logical bytes", "Allocated bytes", "Shared hard link", "Error"}); err != nil {
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
		if err := out.Write([]string{s.FullPath(id), kind, strconv.FormatUint(n.Bytes, 10), strconv.FormatUint(n.Allocated, 10), strconv.FormatBool(n.Shared), s.ErrorAt(id)}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}
