//go:build windows

package disk

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	fsctlQueryUSNJournal = 0x900f4
	// FSCTL_READ_USN_JOURNAL uses METHOD_NEITHER (0xB8 | 3); a METHOD_BUFFERED
	// code is rejected with ERROR_INVALID_FUNCTION.
	fsctlReadUSNJournal = 0x900bb
)

// usnJournalDataV0 is USN_JOURNAL_DATA_V0.
type usnJournalData struct {
	ID                                        uint64
	FirstUSN, NextUSN, LowestValidUSN, MaxUSN int64
	MaximumSize, AllocationDelta              uint64
}

// readUSNInput is READ_USN_JOURNAL_DATA_V1: tracking a specific journal ID
// and pinning the record version requires V1; V0 must pass JournalID = 0
// and returns ERROR_INVALID_FUNCTION otherwise.
type readUSNInput struct {
	StartUSN                         int64
	ReasonMask                       uint32
	ReturnOnlyOnClose                uint32
	Timeout                          uint64
	BytesToWaitFor                   uint64
	JournalID                        uint64
	MinMajorVersion, MaxMajorVersion uint16
}

func openVolume(root string) (*os.File, error) {
	name, e := syscall.UTF16PtrFromString(`\\.\` + root[:2])
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), root), nil
}

func queryJournal(vol *os.File) (usnJournalData, error) {
	var data usnJournalData
	var returned uint32
	buf := make([]byte, 128)
	ok, _, e := deviceIO.Call(vol.Fd(), fsctlQueryUSNJournal, 0, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&returned)), 0)
	if ok == 0 || returned < 56 {
		return data, fmt.Errorf("USN journal query failed: %v", e)
	}
	u64 := binary.LittleEndian.Uint64
	data.ID = u64(buf)
	data.FirstUSN = int64(u64(buf[8:]))
	data.NextUSN = int64(u64(buf[16:]))
	data.LowestValidUSN = int64(u64(buf[24:]))
	data.MaxUSN = int64(u64(buf[32:]))
	data.MaximumSize = u64(buf[40:])
	data.AllocationDelta = u64(buf[48:])
	return data, nil
}

// Watcher tails the NTFS change journal for one volume. Next returns batches
// of events; ApplyUSN folds them into the snapshot.
type Watcher struct {
	vol  *os.File
	id   uint64
	next int64
	buf  []byte
}

// WatchJournal starts a change-journal watcher for a snapshot that recorded
// a journal baseline at scan start (volume-root MFT scans only).
func (s *Snapshot) WatchJournal() (*Watcher, error) {
	if s == nil || s.JournalID == 0 {
		return nil, errors.New("no USN journal baseline; rescan as administrator")
	}
	root := s.FullPath(s.Root)
	vol, err := openVolume(root)
	if err != nil {
		return nil, err
	}
	return &Watcher{vol: vol, id: s.JournalID, next: s.JournalUSN, buf: make([]byte, 128<<10)}, nil
}

func (w *Watcher) Close() { _ = w.vol.Close() }

var errJournalLost = errors.New("USN journal overflowed or was recreated")

// Next blocks for roughly a second and returns newly written journal
// records, translated to USNEvent batches.
func (w *Watcher) Next(ctx context.Context) ([]USNEvent, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		events, err := w.poll()
		if err != nil {
			return nil, err
		}
		if len(events) > 0 {
			return events, nil
		}
	}
}

func (w *Watcher) poll() ([]USNEvent, error) {
	in := readUSNInput{StartUSN: w.next, ReasonMask: 0xffffffff, ReturnOnlyOnClose: 1, Timeout: 1, BytesToWaitFor: 4096, JournalID: w.id, MinMajorVersion: 2, MaxMajorVersion: 2}
	var returned uint32
	ok, _, e := deviceIO.Call(w.vol.Fd(), fsctlReadUSNJournal, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in), uintptr(unsafe.Pointer(&w.buf[0])), uintptr(len(w.buf)), uintptr(unsafe.Pointer(&returned)), 0)
	if ok == 0 {
		return nil, fmt.Errorf("USN journal read failed: %w", e)
	}
	if returned < 8 {
		return nil, nil
	}
	u32, u64 := binary.LittleEndian.Uint32, binary.LittleEndian.Uint64
	next := int64(u64(w.buf))
	var events []USNEvent
	for at := 8; at+60 <= int(returned); {
		rec := w.buf[at:int(returned)]
		length := int(u32(rec))
		if length < 60 || at+length > int(returned) {
			break
		}
		at += length
		if major := binary.LittleEndian.Uint16(rec[4:]); major != 2 {
			continue
		}
		nameLen := int(binary.LittleEndian.Uint16(rec[56:]))
		nameOff := int(binary.LittleEndian.Uint16(rec[58:]))
		if nameOff+nameLen > length {
			continue
		}
		u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&rec[nameOff])), nameLen/2)
		events = append(events, USNEvent{
			FRN:       u64(rec[8:]),
			ParentFRN: u64(rec[16:]),
			Name:      syscall.UTF16ToString(u16),
			Attrs:     u32(rec[52:]),
			Reason:    u32(rec[40:]),
		})
	}
	w.next = next
	// Filtered reasons legitimately skip USNs, so gaps in record numbers
	// prove nothing; only a journal wrap or recreation invalidates the tail.
	j, err := queryJournal(w.vol)
	if err != nil {
		return nil, err
	}
	if j.ID != w.id || j.LowestValidUSN > w.next {
		return nil, errJournalLost
	}
	return events, nil
}

// defaultUSNStat measures an entry the way the directory walk does:
// EndOfFile for logical size and AllocationSize for allocated bytes.
func defaultUSNStat(path string) (uint64, uint64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, false
	}
	if info.IsDir() {
		return 0, 0, true
	}
	alloc, _ := allocation(path, info.Size())
	return uint64(max(info.Size(), 0)), alloc, true
}
