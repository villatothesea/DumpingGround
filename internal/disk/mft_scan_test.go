//go:build windows

package disk

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

// These helpers build synthetic NTFS FILE records so the scan policy in
// scanMFTRecords can be exercised without volume access.

func mftResidentAttr(kind uint32, value []byte) []byte {
	a := make([]byte, 24+len(value))
	binary.LittleEndian.PutUint32(a[0:], kind)
	binary.LittleEndian.PutUint32(a[4:], uint32(len(a)))
	binary.LittleEndian.PutUint32(a[16:], uint32(len(value)))
	binary.LittleEndian.PutUint16(a[20:], 24)
	copy(a[24:], value)
	return a
}

func mftNonresidentData(runs []byte, allocated, logical uint64) []byte {
	a := make([]byte, 64+len(runs))
	binary.LittleEndian.PutUint32(a[0:], 0x80)
	binary.LittleEndian.PutUint32(a[4:], uint32(len(a)))
	a[8] = 1
	binary.LittleEndian.PutUint16(a[32:], 64)
	binary.LittleEndian.PutUint64(a[40:], allocated)
	binary.LittleEndian.PutUint64(a[48:], logical)
	copy(a[64:], runs)
	return a
}

func mftStdInfo() []byte {
	v := make([]byte, 48)
	binary.LittleEndian.PutUint32(v[32:], 0x20) // FILE_ATTRIBUTE_ARCHIVE
	return mftResidentAttr(0x10, v)
}

func mftFileName(parent uint64, namespace byte, name string) []byte {
	chars := utf16.Encode([]rune(name))
	v := make([]byte, 66+2*len(chars))
	binary.LittleEndian.PutUint64(v[0:], parent)
	v[64] = byte(len(chars))
	v[65] = namespace
	for i, ch := range chars {
		binary.LittleEndian.PutUint16(v[66+2*i:], ch)
	}
	return mftResidentAttr(0x30, v)
}

func mftRecord(sector, recordBytes int, sequence uint16, dir bool, attrs ...[]byte) []byte {
	b := make([]byte, recordBytes)
	copy(b[:4], "FILE")
	sectors := recordBytes / sector
	binary.LittleEndian.PutUint16(b[4:], 48)
	binary.LittleEndian.PutUint16(b[6:], uint16(sectors+1))
	binary.LittleEndian.PutUint16(b[16:], sequence)
	off := 48 + 2*(sectors+1)
	binary.LittleEndian.PutUint16(b[20:], uint16(off))
	flags := uint16(1)
	if dir {
		flags |= 2
	}
	binary.LittleEndian.PutUint16(b[22:], flags)
	for _, a := range attrs {
		copy(b[off:], a)
		off += len(a)
	}
	binary.LittleEndian.PutUint32(b[off:], 0xffffffff)
	off += 4
	binary.LittleEndian.PutUint32(b[24:], uint32(off))
	binary.LittleEndian.PutUint32(b[28:], uint32(recordBytes))
	// Update sequence array: marker plus the bytes replaced in each trailer.
	const marker = 0xaaaa
	binary.LittleEndian.PutUint16(b[48:], marker)
	for i := 1; i <= sectors; i++ {
		binary.LittleEndian.PutUint16(b[48+2*i:], 0x1000+uint16(i))
		binary.LittleEndian.PutUint16(b[i*sector-2:], marker)
	}
	return b
}

func TestMFTRecordsSkipBadEntries(t *testing.T) {
	sector, recordBytes := 512, 1024
	root := `D:\`
	var stream bytes.Buffer
	blank := make([]byte, recordBytes)
	for i := 0; i < 5; i++ {
		stream.Write(blank)
	}
	// Record 5: the root directory.
	stream.Write(mftRecord(sector, recordBytes, 1, true, mftStdInfo(), mftFileName(5|1<<48, 1, ".")))
	for i := 6; i < 16; i++ {
		stream.Write(blank)
	}
	// Record 16: child directory. Record 17: file with nonresident data.
	stream.Write(mftRecord(sector, recordBytes, 1, true, mftStdInfo(), mftFileName(5|1<<48, 1, "child")))
	stream.Write(mftRecord(sector, recordBytes, 1, false, mftStdInfo(), mftFileName(16|1<<48, 1, "large.bin"),
		mftNonresidentData([]byte{0x11, 0x10, 0x20, 0x00}, 8192, 8000)))
	// Record 18: file with no unnamed $DATA (EFS-style). Indexed at zero size.
	stream.Write(mftRecord(sector, recordBytes, 1, false, mftStdInfo(), mftFileName(5|1<<48, 1, "locked.dat")))
	// Record 19: FILE magic but a corrupt attribute length.
	bad := mftRecord(sector, recordBytes, 1, false, mftStdInfo(), mftFileName(5|1<<48, 1, "ok"))
	attrAt := 48 + 2*(recordBytes/sector+1)
	binary.LittleEndian.PutUint32(bad[attrAt+4:], 4) // length < 24 fails decode
	stream.Write(bad)
	// Record 20: impossible filename.
	stream.Write(mftRecord(sector, recordBytes, 1, false, mftStdInfo(), mftFileName(5|1<<48, 1, `bad/name`)))

	count := int64(stream.Len() / recordBytes)
	out, err := scanMFTRecords(context.Background(), bytes.NewReader(stream.Bytes()), count, recordBytes, sector, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != "NTFS MFT" || out.Files != 2 || out.Dirs != 2 {
		t.Fatalf("counts: files=%d dirs=%d", out.Files, out.Dirs)
	}
	if out.Estimated != 1 {
		t.Fatalf("unknown-data file not counted as estimate: %d", out.Estimated)
	}
	if out.Skipped < 2 {
		t.Fatalf("corrupt record and bad name not skipped: %d", out.Skipped)
	}
	var names []string
	for _, n := range out.Nodes {
		names = append(names, n.Name)
	}
	joined := strings.Join(names, "|")
	for _, want := range []string{"child", "large.bin", "locked.dat"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "bad/name") {
		t.Fatalf("corrupt name indexed: %q", joined)
	}
	if got := out.Nodes[out.Root].Allocated; got != 8192 {
		t.Fatalf("root allocated=%d, want 8192", got)
	}
}

func TestMFTRecordsCancelKeepsPartial(t *testing.T) {
	sector, recordBytes := 512, 1024
	var stream bytes.Buffer
	for i := 0; i < 5; i++ {
		stream.Write(make([]byte, recordBytes))
	}
	stream.Write(mftRecord(sector, recordBytes, 1, true, mftStdInfo(), mftFileName(5|1<<48, 1, ".")))
	// Enough records to span several 4 MiB decode chunks (4096 records each).
	for stream.Len() < recordBytes*5000 {
		stream.Write(make([]byte, recordBytes))
	}
	ctx, cancel := context.WithCancel(context.Background())
	reads := 0
	src := readerFunc(func(p []byte) (int, error) {
		reads++
		if reads == 1 {
			cancel() // cancel while the first decode chunk was already consumed
		}
		return stream.Read(p)
	})
	out, err := scanMFTRecords(ctx, src, int64(stream.Len()/recordBytes), recordBytes, sector, `D:\`, nil)
	if err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
	if out == nil || out.Nodes[out.Root].Path != `D:\` {
		t.Fatalf("no partial snapshot: %v", out)
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }
