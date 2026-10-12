//go:build windows

package hwmon

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"unsafe"
)

// MIB_IF_ROW2 (x64) is 1352 bytes; these are the offsets of the fields
// used here, checked against a live table by TestLive.
const (
	ifRowSize    = 1352
	ifOffIndex   = 8
	ifOffAlias   = 28
	ifOffDesc    = 542
	ifOffMACLen  = 1056
	ifOffMAC     = 1060
	ifOffMTU     = 1124
	ifOffType    = 1128
	ifOffFlags   = 1152 // bit 0: HardwareInterface
	ifOffOper    = 1156 // 1 = up
	ifOffTxSpeed = 1192
	ifOffRxSpeed = 1200
	ifOffInOct   = 1208
	ifOffInDisc  = 1232
	ifOffInErr   = 1240
	ifOffOutOct  = 1280
	ifOffOutDisc = 1304
	ifOffOutErr  = 1312
)

type ifRow struct {
	Index                   uint32
	Alias, Desc, MAC        string
	MTU, Type               uint32
	Hardware, Up            bool
	RxBps                   uint64
	InOctets, OutOctets     uint64
	InErrors, OutErrors     uint64
	InDiscards, OutDiscards uint64
}

type nicPrev struct {
	in, out uint64
	at      int64 // unix nanos
}

// readIfTable copies GetIfTable2's rows into Go memory and frees the
// system's table.
func readIfTable() []ifRow {
	var tbl unsafe.Pointer
	if r, _, _ := procGetIfTable2.Call(uintptr(unsafe.Pointer(&tbl))); r != 0 || tbl == nil {
		return nil
	}
	defer procFreeMibTable.Call(uintptr(tbl))
	n := int(*(*uint32)(tbl))
	if n <= 0 || n > 4096 {
		return nil
	}
	raw := unsafe.Slice((*byte)(unsafe.Add(tbl, 8)), n*ifRowSize)
	rows := make([]ifRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, parseIfRow(raw[i*ifRowSize:(i+1)*ifRowSize]))
	}
	return rows
}

func parseIfRow(r []byte) ifRow {
	macLen := min(int(le32(r[ifOffMACLen:])), 32)
	mac := make([]string, macLen)
	for j := range mac {
		mac[j] = fmt.Sprintf("%02X", r[ifOffMAC+j])
	}
	return ifRow{
		Index: le32(r[ifOffIndex:]),
		Alias: utf16Bytes(r[ifOffAlias : ifOffAlias+514]),
		Desc:  utf16Bytes(r[ifOffDesc : ifOffDesc+514]),
		MAC:   strings.Join(mac, "-"),
		MTU:   le32(r[ifOffMTU:]), Type: le32(r[ifOffType:]),
		Hardware: r[ifOffFlags]&1 != 0, Up: le32(r[ifOffOper:]) == 1,
		RxBps:    le64(r[ifOffRxSpeed:]),
		InOctets: le64(r[ifOffInOct:]), OutOctets: le64(r[ifOffOutOct:]),
		InErrors: le64(r[ifOffInErr:]), OutErrors: le64(r[ifOffOutErr:]),
		InDiscards: le64(r[ifOffInDisc:]), OutDiscards: le64(r[ifOffOutDisc:]),
	}
}

// readIfRow reads one adapter by interface index — much cheaper than
// building the whole table every second.
func readIfRow(index uint32) (ifRow, bool) {
	var raw [ifRowSize]byte
	binary.LittleEndian.PutUint32(raw[ifOffIndex:], index)
	if r, _, _ := procGetIfEntry2.Call(uintptr(unsafe.Pointer(&raw[0]))); r != 0 {
		return ifRow{}, false
	}
	return parseIfRow(raw[:]), true
}

func nicKind(t uint32) string {
	switch t {
	case 6:
		return "Ethernet"
	case 71:
		return "Wi-Fi"
	case 243, 244:
		return "Cellular"
	}
	return "Network"
}

func (p *platform) loadNICs(info *Info) {
	p.nics = map[uint32]nicPrev{}
	info.NICs = p.enumNICs()
}

// enumNICs lists the hardware adapters that are connected; when none is,
// the first hardware adapter stands in so the page is never network-less.
func (p *platform) enumNICs() (nics []NICInfo) {
	var first *ifRow
	rows := readIfTable()
	for i := range rows {
		r := &rows[i]
		if !r.Hardware || (r.Type != 6 && r.Type != 71 && r.Type != 243 && r.Type != 244) {
			continue
		}
		if first == nil {
			first = r
		}
		if r.Up {
			nics = append(nics, nicInfo(r))
		}
	}
	if len(nics) == 0 && first != nil {
		nics = append(nics, nicInfo(first))
	}
	return nics
}

func nicInfo(r *ifRow) NICInfo {
	n := NICInfo{Index: r.Index, Name: r.Alias, Description: r.Desc, Kind: nicKind(r.Type), MAC: r.MAC, MTU: r.MTU}
	if ifc, err := net.InterfaceByIndex(int(r.Index)); err == nil {
		if addrs, err := ifc.Addrs(); err == nil {
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLinkLocalUnicast() {
					n.IPs = append(n.IPs, ipn.IP.String())
				}
			}
		}
	}
	return n
}

func (p *platform) sampleNICs(s *Sample, info *Info) {
	if len(info.NICs) == 0 {
		return
	}
	s.NICs = make([]NICSample, len(info.NICs))
	for i, n := range info.NICs {
		ns := &s.NICs[i]
		ns.Index = n.Index
		r, ok := readIfRow(n.Index)
		if !ok {
			continue
		}
		ns.Up, ns.LinkBps = r.Up, r.RxBps
		ns.InTotal, ns.OutTotal = r.InOctets, r.OutOctets
		ns.InErrors, ns.OutErrors = r.InErrors, r.OutErrors
		ns.InDiscards, ns.OutDiscards = r.InDiscards, r.OutDiscards
		now := s.At.UnixNano()
		if pr, ok := p.nics[n.Index]; ok && now > pr.at && r.InOctets >= pr.in && r.OutOctets >= pr.out {
			sec := float64(now-pr.at) / 1e9
			ns.InRate = uint64(float64(r.InOctets-pr.in) / sec)
			ns.OutRate = uint64(float64(r.OutOctets-pr.out) / sec)
		}
		p.nics[n.Index] = nicPrev{in: r.InOctets, out: r.OutOctets, at: now}
	}
}
