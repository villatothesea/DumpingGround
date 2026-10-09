//go:build windows

package disk

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/dumpingground/dumpingground/internal/disk/ntfs"
)

var deviceIO = syscall.NewLazyDLL("kernel32.dll").NewProc("DeviceIoControl")

// fastVolumeScan is a read-only MFT path for whole NTFS volumes. Unsupported
// attribute-list layouts, access denial and inconsistent metadata all cause the
// caller to fall back to directory enumeration. No driver or elevation is used.
func fastVolumeScan(ctx context.Context, root string, progress chan<- Progress) (*Snapshot, error) {
	if len(root) != 3 || root[1:] != ":\\" {
		return nil, errors.New("folder scan")
	}
	name, e := syscall.UTF16PtrFromString(`\\.\` + root[:2])
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if e != nil {
		return nil, fmt.Errorf("MFT access unavailable: %w", e)
	}
	volume := os.NewFile(uintptr(h), root)
	if volume == nil {
		syscall.CloseHandle(h)
		return nil, errors.New("invalid volume handle")
	}
	defer volume.Close()
	var data [96]byte
	var returned uint32
	ok, _, e := deviceIO.Call(uintptr(h), 0x90064, 0, 0, uintptr(unsafe.Pointer(&data[0])), 96, uintptr(unsafe.Pointer(&returned)), 0)
	if ok == 0 || returned < 80 {
		return nil, fmt.Errorf("NTFS volume information unavailable: %v", e)
	}
	u32 := binary.LittleEndian.Uint32
	u64 := binary.LittleEndian.Uint64
	sector, cluster, recordBytes := int(u32(data[40:])), int64(u32(data[44:])), int(u32(data[48:]))
	valid, mftLCN := int64(u64(data[56:])), int64(u64(data[64:]))
	clusters := int64(u64(data[16:]))
	if sector < 256 || sector > 4096 || cluster < int64(sector) || recordBytes < sector || recordBytes > 65536 || recordBytes%sector != 0 || valid < 0 || mftLCN < 0 || clusters <= 0 {
		return nil, ntfs.ErrUnsupported
	}
	if mftLCN >= clusters || clusters > int64(^uint64(0)>>1)/cluster {
		return nil, ntfs.ErrUnsupported
	}
	count := valid / int64(recordBytes)
	if count < 16 || count > 8_000_000 {
		return nil, ntfs.ErrUnsupported
	}
	first := make([]byte, recordBytes)
	if _, e = volume.ReadAt(first, mftLCN*cluster); e != nil {
		return nil, e
	}
	mft, e := ntfs.Decode(first, sector)
	if e != nil {
		return nil, e
	}
	if mft.AttributeList || len(mft.Runs) == 0 {
		return nil, fmt.Errorf("%w: extended MFT run list", ntfs.ErrUnsupported)
	}
	var readers []io.Reader
	var total int64
	for _, run := range mft.Runs {
		if run.LCN < 0 || run.Clusters <= 0 || run.LCN >= clusters || run.Clusters > clusters-run.LCN {
			return nil, ntfs.ErrUnsupported
		}
		bytes := run.Clusters * cluster
		readers = append(readers, io.NewSectionReader(volume, run.LCN*cluster, bytes))
		total += bytes
	}
	if total < valid {
		return nil, errors.New("incomplete MFT mapping")
	}
	stream := io.MultiReader(readers...)
	start := time.Now()
	out := &Snapshot{Method: "NTFS MFT"}
	refs := make([]int32, int(count))
	parents := make([]uint64, 0)
	sequences := make([]uint16, 0)
	buf := make([]byte, recordBytes*max(1, 262144/recordBytes))
	index := int64(0)
	last := time.Time{}
	report := func() {
		if progress != nil && time.Since(last) > 100*time.Millisecond {
			last = time.Now()
			p := Progress{Files: out.Files, Dirs: out.Dirs, Bytes: out.Bytes, Allocated: out.Allocated, Skipped: out.Skipped, Current: fmt.Sprintf("%s · NTFS records %d / %d", root, index, count), Elapsed: time.Since(start), Method: "NTFS MFT"}
			select {
			case progress <- p:
			default:
			}
		}
	}
	scanErr := error(nil)
	for index < count {
		if scanErr = ctx.Err(); scanErr != nil {
			break
		}
		n := min(int64(len(buf)), (count-index)*int64(recordBytes))
		if _, e = io.ReadFull(stream, buf[:n]); e != nil {
			return nil, e
		}
		for offset := 0; offset < int(n); offset += recordBytes {
			number := index
			index++
			raw := buf[offset : offset+recordBytes]
			if string(raw[:4]) != "FILE" || u32(raw[22:])&1 == 0 {
				continue
			}
			r, e := ntfs.Decode(raw, sector)
			if e != nil {
				return nil, e
			}
			if r.Base != 0 {
				continue
			}
			if number < 16 && number != 5 {
				continue
			}
			if r.Attributes&(0x400|0x1000|0x40000|0x400000) != 0 {
				out.Skipped++
				continue
			}
			if !r.Directory && !r.DataKnown {
				return nil, fmt.Errorf("%w: external data attributes", ntfs.ErrUnsupported)
			}
			hasLong := false
			for _, name := range r.Names {
				if name.Namespace != 2 {
					hasLong = true
				}
			}
			physicalCounted := false
			namesSeen := map[string]bool{}
			for _, name := range r.Names {
				if name.Namespace == 2 && hasLong {
					continue
				}
				if name.Text == "" || strings.ContainsAny(name.Text, `\/:`) {
					return nil, errors.New("invalid NTFS filename")
				}
				key := fmt.Sprintf("%d/%s", name.Parent, name.Text)
				if namesSeen[key] {
					continue
				}
				namesSeen[key] = true
				id := len(out.Nodes)
				node := Node{ID: id, Parent: -1, Name: name.Text, Dir: r.Directory}
				if number == 5 {
					out.Root = id
					node.Name = root
					node.Path = root
				}
				if !r.Directory {
					node.Bytes = r.Logical
					node.Allocated = r.Allocated
					if physicalCounted {
						node.Allocated = 0
						node.Shared = true
					}
					physicalCounted = true
					out.Files++
					out.Bytes += node.Bytes
					out.Allocated += node.Allocated
				} else {
					out.Dirs++
				}
				if refs[number] == 0 {
					refs[number] = int32(id + 1)
				}
				out.Nodes = append(out.Nodes, node)
				parents = append(parents, name.Parent)
				sequences = append(sequences, r.Sequence)
			}
			report()
		}
	}
	if len(out.Nodes) == 0 || out.Nodes[out.Root].Path != root {
		return nil, errors.New("root record missing from MFT")
	}
	// Link using both record index and sequence number. Stale directory references
	// are excluded rather than attached to a newly reused MFT record.
	for i, ref := range parents {
		if i == out.Root {
			continue
		}
		pnum := ref & 0x0000ffffffffffff
		if pnum >= uint64(len(refs)) || refs[pnum] == 0 {
			out.Skipped++
			continue
		}
		parent := int(refs[pnum]) - 1
		if parent == i || !out.Nodes[parent].Dir || sequences[parent] != uint16(ref>>48) {
			out.Skipped++
			continue
		}
		out.Nodes[i].Parent = parent
		out.Nodes[parent].Children = append(out.Nodes[parent].Children, i)
	}
	// Traverse reachable nodes, count only them, and aggregate in true post-order;
	// parent record numbers do not necessarily precede their children.
	order := []int{out.Root}
	seen := make([]bool, len(out.Nodes))
	seen[out.Root] = true
	out.Files, out.Dirs, out.Bytes, out.Allocated = 0, 0, 0, 0
	for at := 0; at < len(order); at++ {
		id := order[at]
		n := &out.Nodes[id]
		if n.Dir {
			out.Dirs++
		} else {
			out.Files++
			out.Bytes += n.Bytes
			out.Allocated += n.Allocated
		}
		for _, kid := range n.Children {
			if seen[kid] {
				return nil, errors.New("cyclic MFT directory graph")
			}
			seen[kid] = true
			order = append(order, kid)
		}
	}
	for i := len(order) - 1; i > 0; i-- {
		n := &out.Nodes[order[i]]
		p := &out.Nodes[n.Parent]
		p.Bytes += n.Bytes
		p.Allocated += n.Allocated
	}
	for _, id := range order {
		kids := out.Nodes[id].Children
		sort.Slice(kids, func(a, b int) bool { return out.Nodes[kids[a]].Allocated > out.Nodes[kids[b]].Allocated })
	}
	out.Duration = time.Since(start)
	return out, scanErr
}
