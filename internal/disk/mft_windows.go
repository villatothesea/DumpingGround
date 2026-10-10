//go:build windows

package disk

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

// scanMFTRecords decodes the MFT stream into a snapshot. The stream is read
// serially but records decode in parallel across cores — the per-record
// parse, not the sequential disk read, is what costs on a big volume.
// Individual bad or unusual records are counted like walk skips instead of
// failing the whole volume; only conditions that make the snapshot
// meaningless return an error.
func scanMFTRecords(ctx context.Context, stream io.Reader, count int64, recordBytes, sector int, root string, progress chan<- Progress) (*Snapshot, error) {
	u16 := binary.LittleEndian.Uint16
	u32 := binary.LittleEndian.Uint32
	start := time.Now()
	out := &Snapshot{Method: "NTFS MFT"}
	refs := make([]int32, int(count))

	// Chunk the stream so workers decode disjoint record ranges in parallel.
	// Chunk buffers rotate through a small pool because the collector is
	// paused for the scan and chunk allocations would otherwise pile up.
	perChunk := max(1, (4<<20)/recordBytes)
	type work struct {
		seq   int
		first int64
		recs  int
		buf   []byte
	}
	type part struct {
		first   int64
		recs    int
		nodes   []Node
		parents []uint64
		seqs    []uint16
		rootAt  int
		stats   [8]uint64 // files, dirs, bytes, allocated, skipped, reparse, offline, estimated
	}
	workers := min(runtime.GOMAXPROCS(0), 16)
	nChunks := int((count + int64(perChunk) - 1) / int64(perChunk))
	parts := make([]part, nChunks)
	for i := range parts {
		parts[i].rootAt = -1 // undispatched chunks stay -1, never a root
	}
	jobs := make(chan work, workers)
	// A fixed pool of chunk buffers: the reader blocks on the pool instead
	// of allocating past it, so workers can always hand a buffer back.
	free := make(chan []byte, workers)
	for i := 0; i < workers; i++ {
		free <- make([]byte, perChunk*recordBytes)
	}
	var completed atomic.Int64
	var progFiles, progDirs, progBytes, progAllocated, progSkipped atomic.Uint64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var seen []int
			for j := range jobs {
				p := part{first: j.first, recs: j.recs, rootAt: -1}
				for i := 0; i < j.recs; i++ {
					number := j.first + int64(i)
					raw := j.buf[i*recordBytes : (i+1)*recordBytes]
					if u32(raw[:4]) != 0x454c4946 || u16(raw[22:])&1 == 0 {
						continue
					}
					r, e := ntfs.DecodeMutable(raw, sector)
					if e != nil {
						// One torn or corrupt record must not abort a scan
						// of millions: skip it, the way the directory walk
						// skips entries it cannot open.
						p.stats[4]++
						continue
					}
					if r.Base != 0 {
						continue
					}
					if number < 16 && number != 5 {
						continue
					}
					if r.Attributes&(0x400|0x1000|0x40000|0x400000) != 0 {
						p.stats[4]++
						if r.Attributes&0x400 != 0 {
							p.stats[5]++
						} else {
							p.stats[6]++
						}
						continue
					}
					hasLong := false
					for _, name := range r.Names {
						if name.Namespace != 2 {
							hasLong = true
						}
					}
					// Records without an unnamed $DATA stream keep their
					// data elsewhere: EFS files, attribute lists held by
					// extension records, named streams only. They still
					// exist on disk, so index them at unknown size instead
					// of abandoning the volume.
					unknownData := !r.Directory && !r.DataKnown
					physicalCounted := false
					indexed := len(p.nodes)
					seen = seen[:0]
					for ni, name := range r.Names {
						if name.Namespace == 2 && hasLong {
							continue
						}
						if name.Text == "" || strings.ContainsAny(name.Text, `\/:`) {
							// An impossible name means a corrupt record;
							// keep the names already decoded and move on.
							p.stats[4]++
							break
						}
						dup := false
						for _, pi := range seen {
							if prev := r.Names[pi]; prev.Parent == name.Parent && prev.Text == name.Text {
								dup = true
								break
							}
						}
						if dup {
							continue
						}
						seen = append(seen, ni)
						id := len(p.nodes)
						node := Node{ID: id, Parent: -1, Name: name.Text, Dir: r.Directory, FRN: uint64(number) | uint64(r.Sequence)<<48}
						if number == 5 {
							p.rootAt = id
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
							p.stats[0]++
							p.stats[2] += node.Bytes
							p.stats[3] += node.Allocated
						} else {
							p.stats[1]++
						}
						if refs[number] == 0 {
							refs[number] = int32(id + 1)
						}
						p.nodes = append(p.nodes, node)
						p.parents = append(p.parents, name.Parent)
						p.seqs = append(p.seqs, r.Sequence)
					}
					if unknownData && len(p.nodes) > indexed {
						p.stats[7]++
					}
				}
				parts[j.seq] = p
				completed.Add(int64(j.recs))
				progFiles.Add(p.stats[0])
				progDirs.Add(p.stats[1])
				progBytes.Add(p.stats[2])
				progAllocated.Add(p.stats[3])
				progSkipped.Add(p.stats[4])
				free <- j.buf
			}
		}()
	}
	last := time.Time{}
	report := func(force bool) {
		if progress == nil || (!force && time.Since(last) < 100*time.Millisecond) {
			return
		}
		last = time.Now()
		done := completed.Load()
		p := Progress{Files: progFiles.Load(), Dirs: progDirs.Load(), Bytes: progBytes.Load(), Allocated: progAllocated.Load(), Skipped: progSkipped.Load(), Current: fmt.Sprintf("%s · NTFS records %s / %s", root, format.Int(uint64(done)), format.Int(uint64(count))), Elapsed: time.Since(start), Method: "NTFS MFT"}
		select {
		case progress <- p:
		default:
		}
	}
	scanErr := error(nil)
	readErr := error(nil)
	seq, first := 0, int64(0)
	for first < count {
		if scanErr = ctx.Err(); scanErr != nil {
			break
		}
		recs := min(perChunk, int(count-first))
		buf := <-free
		buf = buf[:recs*recordBytes]
		if _, e := io.ReadFull(stream, buf); e != nil {
			readErr = e
			break
		}
		jobs <- work{seq: seq, first: first, recs: recs, buf: buf}
		first += int64(recs)
		seq++
		report(false)
	}
	// A cancel that landed while the final chunk was in flight still counts:
	// the snapshot then carries every record read so far, same as a mid-scan
	// break.
	if scanErr == nil {
		scanErr = ctx.Err()
	}
	close(jobs)
	wg.Wait()
	if readErr != nil {
		return nil, readErr
	}
	// Merge chunks in record order: node IDs, parent links and ref slots all
	// key on that order.
	total := 0
	for c := range parts {
		total += len(parts[c].nodes)
	}
	out.Nodes = make([]Node, 0, total)
	parents := make([]uint64, 0, total)
	sequences := make([]uint16, 0, total)
	for c := range parts {
		p := &parts[c]
		base := len(out.Nodes)
		out.Nodes = append(out.Nodes, p.nodes...)
		parents = append(parents, p.parents...)
		sequences = append(sequences, p.seqs...)
		for i := base; i < len(out.Nodes); i++ {
			out.Nodes[i].ID = i
		}
		if p.rootAt >= 0 {
			out.Root = base + p.rootAt
		}
		for num := p.first; num < p.first+int64(p.recs) && num < count; num++ {
			if refs[num] != 0 {
				refs[num] += int32(base)
			}
		}
		out.Files += p.stats[0]
		out.Dirs += p.stats[1]
		out.Bytes += p.stats[2]
		out.Allocated += p.stats[3]
		out.Skipped += p.stats[4]
		out.Reparse += p.stats[5]
		out.Offline += p.stats[6]
		out.Estimated += p.stats[7]
	}
	report(true)
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
		slices.SortFunc(kids, func(a, b int) int { return cmp.Compare(out.Nodes[b].Allocated, out.Nodes[a].Allocated) })
	}
	// Append growth overshoots by up to 2×; hand the spare capacity back —
	// a full volume holds on the order of a million nodes.
	out.Nodes = slices.Clip(out.Nodes)
	out.Duration = time.Since(start)
	return out, scanErr
}
