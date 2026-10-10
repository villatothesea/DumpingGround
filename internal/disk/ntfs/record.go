// Package ntfs decodes the public NTFS on-disk record format. It performs no
// writes and deliberately rejects layouts it cannot account for reliably.
package ntfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

var ErrUnsupported = errors.New("unsupported NTFS metadata layout")

type Run struct{ LCN, Clusters int64 }
type Name struct {
	Parent    uint64
	Text      string
	Namespace byte
}
type Record struct {
	Sequence           uint16
	Directory          bool
	Base               uint64
	Names              []Name
	Logical, Allocated uint64
	Attributes         uint32
	Runs               []Run
	AttributeList      bool
	DataKnown          bool
}

func u16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func u32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func u64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

// Decode fixes sector trailers in a private copy; callers may reuse raw buffers.
func Decode(raw []byte, sector int) (Record, error) {
	return decode(raw, sector, false)
}

// DecodeMutable is Decode for callers who own raw: the update sequence
// array is applied in place, saving a copy of every record.
func DecodeMutable(raw []byte, sector int) (Record, error) {
	return decode(raw, sector, true)
}

func decode(raw []byte, sector int, mutate bool) (Record, error) {
	var r Record
	if len(raw) < 48 || u32(raw[:4]) != 0x454c4946 /* "FILE" */ {
		return r, errors.New("not a FILE record")
	}
	if u16(raw[22:])&1 == 0 {
		return r, nil
	}
	b := raw
	if !mutate {
		b = append([]byte(nil), raw...)
	}
	usa, count := int(u16(b[4:])), int(u16(b[6:]))
	if sector < 256 || usa < 8 || count < 2 || usa+count*2 > len(b) || (count-1)*sector != len(b) {
		return r, errors.New("invalid update sequence array")
	}
	marker := u16(b[usa:])
	for i := 1; i < count; i++ {
		at := i*sector - 2
		if u16(b[at:]) != marker {
			return r, errors.New("torn NTFS record")
		}
		copy(b[at:at+2], b[usa+i*2:usa+i*2+2])
	}
	r.Sequence = u16(b[16:])
	r.Directory = u16(b[22:])&2 != 0
	r.Base = u64(b[32:])
	used := int(u32(b[24:]))
	if used > len(b) || used < 48 {
		return r, errors.New("invalid used length")
	}
	for off := int(u16(b[20:])); off+8 <= used; {
		kind := u32(b[off:])
		if kind == 0xffffffff {
			break
		}
		length := int(u32(b[off+4:]))
		if length < 24 || length > used-off {
			return r, errors.New("invalid attribute length")
		}
		a := b[off : off+length]
		nonresident := a[8] != 0
		named := a[9] != 0
		var value []byte
		if !nonresident {
			n, p := int(u32(a[16:])), int(u16(a[20:]))
			if p < 24 || p > len(a) || n > len(a)-p {
				return r, errors.New("invalid resident attribute")
			}
			value = a[p : p+n]
		}
		switch kind {
		case 0x10:
			if len(value) >= 36 {
				r.Attributes = u32(value[32:])
			}
		case 0x20:
			r.AttributeList = true
		case 0x30:
			if len(value) < 66 {
				return r, errors.New("short filename attribute")
			}
			n := int(value[64])
			if 66+n*2 > len(value) {
				return r, errors.New("short filename")
			}
			// A filename is at most 255 UTF-16 units; decode on the stack
			// and let the string conversion make the only allocation.
			var chars [255]uint16
			for i := 0; i < n; i++ {
				chars[i] = u16(value[66+i*2:])
			}
			r.Names = append(r.Names, Name{u64(value), string(utf16.Decode(chars[:n])), value[65]})
		case 0x80:
			if !named {
				if nonresident {
					if len(a) < 64 {
						return r, errors.New("short nonresident attribute")
					}
					// Heavily fragmented data can occupy several attribute
					// segments in one record: the first reports the file's
					// logical size, every segment carries its own allocation.
					if u64(a[16:]) == 0 {
						r.Logical = u64(a[48:])
						r.DataKnown = true
						offset := int(u16(a[32:]))
						if offset < 64 || offset >= len(a) {
							return r, errors.New("invalid mapping pairs")
						}
						var err error
						r.Runs, err = DecodeRuns(a[offset:])
						if err != nil {
							return r, err
						}
					}
					// Sparse/compressed allocation is stored separately in extended headers.
					if u16(a[12:])&0x8001 != 0 {
						if len(a) < 72 {
							return r, ErrUnsupported
						}
						r.Allocated += u64(a[64:])
					} else {
						r.Allocated += u64(a[40:])
					}
				} else {
					r.Logical = uint64(len(value))
					// Resident data sits 8-byte aligned inside the MFT
					// record; FileStandardInfo reports that footprint as
					// AllocationSize, so mirror it rather than counting 0.
					r.Allocated = (r.Logical + 7) &^ 7
					r.DataKnown = true
				}
			}
		}
		off += length
	}
	return r, nil
}
func DecodeRuns(b []byte) ([]Run, error) {
	var out []Run
	var lcn int64
	for p := 0; p < len(b); {
		header := b[p]
		p++
		if header == 0 {
			return out, nil
		}
		n, m := int(header&15), int(header>>4)
		if n < 1 || n > 8 || m > 8 || p+n+m > len(b) {
			return nil, errors.New("invalid mapping pair")
		}
		var clusters uint64
		for i := 0; i < n; i++ {
			clusters |= uint64(b[p+i]) << uint(8*i)
		}
		p += n
		if clusters == 0 || clusters > uint64(^uint64(0)>>1) {
			return nil, errors.New("invalid cluster count")
		}
		if m == 0 {
			out = append(out, Run{-1, int64(clusters)})
			continue
		}
		var delta uint64
		for i := 0; i < m; i++ {
			delta |= uint64(b[p+i]) << uint(i*8)
		}
		if m < 8 && b[p+m-1]&0x80 != 0 {
			delta |= ^uint64(0) << uint(m*8)
		}
		p += m
		next := lcn + int64(delta)
		if next < 0 || (int64(delta) > 0 && next < lcn) {
			return nil, fmt.Errorf("%w: invalid cluster offset", ErrUnsupported)
		}
		lcn = next
		out = append(out, Run{lcn, int64(clusters)})
	}
	return nil, errors.New("unterminated mapping pairs")
}
