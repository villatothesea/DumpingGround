package disk

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// TestMFTScanLargeStream pushes half a million records through the chunked
// parallel decoder: every chunk boundary must merge in record order and the
// totals must line up with what was written.
func TestMFTScanLargeStream(t *testing.T) {
	sector, recordBytes := 512, 1024
	var stream bytes.Buffer
	for i := 0; i < 5; i++ {
		stream.Write(make([]byte, recordBytes))
	}
	stream.Write(mftRecord(sector, recordBytes, 1, true, mftStdInfo(), mftFileName(5|1<<48, 1, ".")))
	n := 500000
	for i := 6; i < n; i++ {
		dir := i%10 == 0
		parent := uint64(5) | 1<<48
		stream.Write(mftRecord(sector, recordBytes, 1, dir, mftStdInfo(), mftFileName(parent, 1, "file-n"), mftNonresidentData([]byte{0x11, 0x01, 0x01, 0x00}, 4096, 4096)))
	}
	count := int64(stream.Len() / recordBytes)
	start := time.Now()
	out, err := scanMFTRecords(context.Background(), bytes.NewReader(stream.Bytes()), count, recordBytes, sector, `D:\`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(out.Nodes), n-16+1; got != want {
		t.Fatalf("nodes=%d, want %d", got, want)
	}
	if out.FullPath(out.Root) != `D:\` {
		t.Fatalf("root path %q", out.FullPath(out.Root))
	}
	files, dirs := int64(0), int64(0)
	for i := 16; i < n; i++ { // records below 16 (except 5) are metadata, skipped
		if i%10 == 0 {
			dirs++
		} else {
			files++
		}
	}
	if out.Files != uint64(files) || out.Dirs != uint64(dirs)+1 {
		t.Fatalf("files=%d dirs=%d, want %d/%d", out.Files, out.Dirs, files, dirs+1)
	}
	t.Logf("records=%d nodes=%d elapsed=%v (%.0f rec/s)", count, len(out.Nodes), time.Since(start), float64(count)/time.Since(start).Seconds())
}
