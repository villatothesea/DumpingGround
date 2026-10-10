//go:build windows

package disk

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/dumpingground/dumpingground/internal/disk/ntfs"
	"github.com/dumpingground/dumpingground/internal/format"
)

var deviceIO = syscall.NewLazyDLL("kernel32.dll").NewProc("DeviceIoControl")

// fastVolumeScan is a read-only MFT path for whole NTFS volumes. Unsupported
// attribute-list layouts, access denial and inconsistent metadata all cause the
// caller to fall back to directory enumeration. No driver or elevation is used.
func fastVolumeScan(ctx context.Context, root string, progress chan<- Progress) (*Snapshot, error) {
	if len(root) != 3 || root[1:] != ":\\" {
		return nil, errors.New("folder scan")
	}
	volume, e := openVolume(root)
	if e != nil {
		return nil, fmt.Errorf("MFT access unavailable: %w", e)
	}
	defer volume.Close()
	// Capture the change-journal position before scanning so a watcher can
	// replay everything that moved during the scan.
	journal, _ := queryJournal(volume)
	var data [96]byte
	var returned uint32
	ok, _, e := deviceIO.Call(volume.Fd(), 0x90064, 0, 0, uintptr(unsafe.Pointer(&data[0])), 96, uintptr(unsafe.Pointer(&returned)), 0)
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
	snap, err := scanMFTRecords(ctx, stream, count, recordBytes, sector, root, progress)
	if snap != nil && journal.ID != 0 {
		snap.JournalID = journal.ID
		snap.JournalUSN = journal.NextUSN
	}
	return snap, err
}

// ProbeMFT exposes the strict MFT read for diagnostics: it never walks the
// directory tree and never falls back, so permission or parsing failures
// surface verbatim.
func ProbeMFT(ctx context.Context, root string) (*Snapshot, error) {
	return fastVolumeScan(ctx, root, nil)
}

// scanMFTRecords decodes the MFT stream into a snapshot. Individual bad or
// unusual records are counted like walk skips instead of failing the whole
// volume; only conditions that make the snapshot meaningless return an error.
func scanMFTRecords(ctx context.Context, stream io.Reader, count int64, recordBytes, sector int, root string, progress chan<- Progress) (*Snapshot, error) {
	u32 := binary.LittleEndian.Uint32
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
			p := Progress{Files: out.Files, Dirs: out.Dirs, Bytes: out.Bytes, Allocated: out.Allocated, Skipped: out.Skipped, Current: fmt.Sprintf("%s · NTFS records %s / %s", root, format.Int(uint64(index)), format.Int(uint64(count))), Elapsed: time.Since(start), Method: "NTFS MFT"}
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
		if _, e := io.ReadFull(stream, buf[:n]); e != nil {
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
				// One torn or corrupt record must not abort a scan of
				// millions: skip it, the way the directory walk skips
				// entries it cannot open.
				out.Skipped++
				continue
			}
			if r.Base != 0 {
				continue
			}
			if number < 16 && number != 5 {
				continue
			}
			if r.Attributes&(0x400|0x1000|0x40000|0x400000) != 0 {
				out.Skipped++
				if r.Attributes&0x400 != 0 {
					out.Reparse++
				} else {
					out.Offline++
				}
				continue
			}
			hasLong := false
			for _, name := range r.Names {
				if name.Namespace != 2 {
					hasLong = true
				}
			}
			// Records without an unnamed $DATA stream keep their data
			// elsewhere: EFS files, attribute lists held by extension
			// records, named streams only. They still exist on disk, so
			// index them at unknown size instead of abandoning the volume.
			unknownData := !r.Directory && !r.DataKnown
			physicalCounted := false
			namesSeen := map[string]bool{}
			indexed := len(out.Nodes)
			for _, name := range r.Names {
				if name.Namespace == 2 && hasLong {
					continue
				}
				if name.Text == "" || strings.ContainsAny(name.Text, `\/:`) {
					// An impossible name means a corrupt record; keep the
					// names already decoded and move to the next record.
					out.Skipped++
					break
				}
				key := fmt.Sprintf("%d/%s", name.Parent, name.Text)
				if namesSeen[key] {
					continue
				}
				namesSeen[key] = true
				id := len(out.Nodes)
				node := Node{ID: id, Parent: -1, Name: name.Text, Dir: r.Directory, FRN: uint64(number) | uint64(r.Sequence)<<48}
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
			if unknownData && len(out.Nodes) > indexed {
				out.Estimated++
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
			out.Unlinked++
			continue
		}
		parent := int(refs[pnum]) - 1
		if parent == i || !out.Nodes[parent].Dir || sequences[parent] != uint16(ref>>48) {
			out.Skipped++
			out.Unlinked++
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
