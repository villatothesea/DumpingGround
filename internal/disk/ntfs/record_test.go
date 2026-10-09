package ntfs

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

var put16 = binary.LittleEndian.PutUint16
var put32 = binary.LittleEndian.PutUint32
var put64 = binary.LittleEndian.PutUint64

func resident(kind uint32, value []byte) []byte {
	n := (24 + len(value) + 7) &^ 7
	a := make([]byte, n)
	put32(a, kind)
	put32(a[4:], uint32(n))
	put32(a[16:], uint32(len(value)))
	put16(a[20:], 24)
	copy(a[24:], value)
	return a
}
func recordFixture(attrs ...[]byte) []byte {
	b := make([]byte, 1024)
	copy(b, "FILE")
	put16(b[4:], 48)
	put16(b[6:], 3)
	put16(b[16:], 7)
	put16(b[18:], 1)
	put16(b[20:], 56)
	put16(b[22:], 1)
	put32(b[28:], 1024)
	p := 56
	for _, a := range attrs {
		copy(b[p:], a)
		p += len(a)
	}
	put32(b[p:], 0xffffffff)
	put32(b[24:], uint32(p+8))
	put16(b[48:], 0xabba)
	copy(b[50:52], b[510:512])
	copy(b[52:54], b[1022:1024])
	put16(b[510:], 0xabba)
	put16(b[1022:], 0xabba)
	return b
}
func TestDecodeResidentUnicodeFile(t *testing.T) {
	chars := utf16.Encode([]rune("文件.txt"))
	name := make([]byte, 66+len(chars)*2)
	put64(name, 5|uint64(9)<<48)
	name[64] = byte(len(chars))
	name[65] = 1
	for i, n := range chars {
		put16(name[66+i*2:], n)
	}
	b := recordFixture(resident(0x30, name), resident(0x80, []byte("hello")))
	r, e := Decode(b, 512)
	if e != nil {
		t.Fatal(e)
	}
	if r.Sequence != 7 || !r.DataKnown || r.Logical != 5 || r.Allocated != 0 || len(r.Names) != 1 || r.Names[0].Text != "文件.txt" || r.Names[0].Parent != 5|uint64(9)<<48 {
		t.Fatalf("record=%+v", r)
	}
	if binary.LittleEndian.Uint16(b[510:]) != 0xabba {
		t.Fatal("decoder mutated input")
	}
	b[510] = 0
	if _, e := Decode(b, 512); e == nil {
		t.Fatal("torn sector accepted")
	}
}
func TestDecodeNonResidentData(t *testing.T) {
	a := make([]byte, 80)
	put32(a, 0x80)
	put32(a[4:], 80)
	a[8] = 1
	put64(a[24:], 2)
	put16(a[32:], 64)
	put64(a[40:], 12288)
	put64(a[48:], 9000)
	copy(a[64:], []byte{0x11, 3, 100, 0})
	r, e := Decode(recordFixture(a), 512)
	if e != nil {
		t.Fatal(e)
	}
	if r.Logical != 9000 || r.Allocated != 12288 || len(r.Runs) != 1 || r.Runs[0] != (Run{100, 3}) {
		t.Fatal(r)
	}
}
func TestMappingPairsSignedDeltasAndSparse(t *testing.T) {
	runs, e := DecodeRuns([]byte{0x11, 3, 100, 0x11, 2, 0xfe, 0x01, 4, 0})
	if e != nil {
		t.Fatal(e)
	}
	want := []Run{{100, 3}, {98, 2}, {-1, 4}}
	if len(runs) != len(want) {
		t.Fatal(runs)
	}
	for i := range runs {
		if runs[i] != want[i] {
			t.Fatal(runs)
		}
	}
	for _, bad := range [][]byte{{0x99}, {0x11, 0, 1, 0}, {0x11, 2, 0xff, 0}, {0x11, 1, 1}} {
		if _, e := DecodeRuns(bad); e == nil {
			t.Fatalf("accepted %x", bad)
		}
	}
}
func TestRejectAttributeOutsideRecord(t *testing.T) {
	b := recordFixture(resident(0x80, nil))
	put32(b[60:], 4096)
	if _, e := Decode(b, 512); e == nil {
		t.Fatal("out-of-bounds attribute accepted")
	}
}
