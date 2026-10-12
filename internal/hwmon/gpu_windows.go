//go:build windows

package hwmon

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	procPdhOpenQuery    = pdhDLL.NewProc("PdhOpenQueryW")
	procPdhAddCounter   = pdhDLL.NewProc("PdhAddEnglishCounterW")
	procPdhCollect      = pdhDLL.NewProc("PdhCollectQueryData")
	procPdhGetArray     = pdhDLL.NewProc("PdhGetFormattedCounterArrayW")
	procPdhCloseQuery   = pdhDLL.NewProc("PdhCloseQuery")
	procCreateDXGIFact1 = dxgiDLL.NewProc("CreateDXGIFactory1")
)

const (
	pdhMoreData  = 0x800007D2
	pdhFmtDouble = 0x200
	pdhNoCap100  = 0x8000
	pdhItemSize  = 24 // PDH_FMT_COUNTERVALUE_ITEM_W on x64
	gpuQueryLife = 30 * time.Second
)

// com helpers: obj is a COM interface pointer, never Go memory, so the
// vtable is walked with unsafe.Add instead of uintptr round-trips.
func comMethod(obj unsafe.Pointer, idx int) uintptr {
	return *(*uintptr)(unsafe.Add(*(*unsafe.Pointer)(obj), idx*8))
}

func comRelease(obj unsafe.Pointer) { syscall.SyscallN(comMethod(obj, 2), uintptr(obj)) }

func (p *platform) loadGPUs(info *Info) {
	if procCreateDXGIFact1.Find() != nil {
		return
	}
	iid := syscall.GUID{Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba, Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
	var factory unsafe.Pointer
	if hr, _, _ := procCreateDXGIFact1.Call(uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&factory))); hr != 0 || factory == nil {
		return
	}
	defer comRelease(factory)
	var idents []string
	for i := 0; i < 16; i++ {
		var ad unsafe.Pointer
		// IDXGIFactory1::EnumAdapters1 — vtable slot 12.
		if hr, _, _ := syscall.SyscallN(comMethod(factory, 12), uintptr(factory), uintptr(i), uintptr(unsafe.Pointer(&ad))); hr != 0 || ad == nil {
			break
		}
		var d [312]byte // DXGI_ADAPTER_DESC1
		// IDXGIAdapter1::GetDesc1 — vtable slot 10.
		hr, _, _ := syscall.SyscallN(comMethod(ad, 10), uintptr(ad), uintptr(unsafe.Pointer(&d[0])))
		comRelease(ad)
		if hr != 0 || le32(d[304:])&2 != 0 { // 2 = DXGI_ADAPTER_FLAG_SOFTWARE
			continue
		}
		luid := fmt.Sprintf("0x%08x_0x%08x", le32(d[300:]), le32(d[296:]))
		// Same vendor/device/subsystem/revision and name = the same
		// physical adapter seen again: fold its LUID into the first.
		ident := fmt.Sprintf("%x-%x-%x-%x", le32(d[256:]), le32(d[260:]), le32(d[264:]), le32(d[268:]))
		if j := slices.IndexFunc(idents, func(s string) bool { return s == ident }); j >= 0 {
			info.GPUs[j].luids = append(info.GPUs[j].luids, luid)
			continue
		}
		idents = append(idents, ident)
		g := GPUInfo{
			Name:           strings.Join(strings.Fields(utf16Bytes(d[:256])), " "),
			Vendor:         gpuVendor(le32(d[256:])),
			DedicatedBytes: le64(d[272:]),
			SharedBytes:    le64(d[288:]),
			luids:          []string{luid},
		}
		g.Driver, g.DriverDate = gpuDriver(g.Name)
		info.GPUs = append(info.GPUs, g)
	}
	if len(info.GPUs) > 0 {
		p.gpu.startOpen() // warm the PDH query while the page is still being laid out
	}
}

func gpuVendor(id uint32) string {
	switch id {
	case 0x10DE:
		return "NVIDIA"
	case 0x1002, 0x1022:
		return "AMD"
	case 0x8086:
		return "Intel"
	case 0x5143:
		return "Qualcomm"
	case 0x1414:
		return "Microsoft"
	}
	return ""
}

// gpuDriver finds the display class key whose DriverDesc matches the
// adapter's name and returns its driver version and date.
func gpuDriver(name string) (version, date string) {
	const class = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}\`
	for i := 0; i < 16; i++ {
		h, ok := regOpen(fmt.Sprintf("%s%04d", class, i))
		if !ok {
			continue
		}
		desc := regStr(h, "DriverDesc")
		if strings.EqualFold(desc, name) {
			version, date = regStr(h, "DriverVersion"), isoDate(regStr(h, "DriverDate"))
			syscall.RegCloseKey(h)
			return
		}
		syscall.RegCloseKey(h)
	}
	return
}

// isoDate turns the registry's "2-19-2026" into "2026-02-19".
func isoDate(s string) string {
	p := strings.Split(s, "-")
	if len(p) != 3 || len(p[2]) != 4 {
		return s
	}
	m, e1 := strconv.Atoi(p[0])
	d, e2 := strconv.Atoi(p[1])
	if e1 != nil || e2 != nil {
		return s
	}
	return fmt.Sprintf("%s-%02d-%02d", p[2], m, d)
}

// pdhQuery is one PDH query over the GPU counters. The wildcard counters
// expand when added, so a long-lived query would never see processes that
// start later: the sampler rotates in a fresh query every gpuQueryLife.
type pdhQuery struct {
	q                    uintptr
	util, dedicated, shr uintptr
	collects             int
	born                 time.Time
}

// gpuState owns the PDH queries. Opening one costs hundreds of
// milliseconds the first time (PDH expands the wildcard counters — up to
// ~2 s on a cold start), so queries open on their own goroutine and are
// handed over through ready; the sampler never waits for them.
type gpuState struct {
	cur, next *pdhQuery
	last      time.Time
	ready     atomic.Pointer[pdhQuery]
	opening   atomic.Bool
	dead      atomic.Bool // the counters do not exist on this system
}

func (g *gpuState) startOpen() {
	if g.dead.Load() || !g.opening.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer g.opening.Store(false)
		if q := openGPUQuery(); q != nil {
			g.ready.Store(q)
		} else {
			g.dead.Store(true)
		}
	}()
}

func openGPUQuery() *pdhQuery {
	if procPdhOpenQuery.Find() != nil {
		return nil
	}
	q := &pdhQuery{born: time.Now()}
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q.q))); r != 0 {
		return nil
	}
	add := func(path string, dst *uintptr) {
		p, _ := syscall.UTF16PtrFromString(path)
		procPdhAddCounter.Call(q.q, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(dst)))
	}
	add(`\GPU Engine(*)\Utilization Percentage`, &q.util)
	add(`\GPU Adapter Memory(*)\Dedicated Usage`, &q.dedicated)
	add(`\GPU Adapter Memory(*)\Shared Usage`, &q.shr)
	if q.util == 0 && q.dedicated == 0 {
		q.close()
		return nil
	}
	return q
}

func (q *pdhQuery) collect() {
	procPdhCollect.Call(q.q)
	q.collects++
}

func (q *pdhQuery) close() { procPdhCloseQuery.Call(q.q) }

// values returns every instance of a wildcard counter by name. PDH puts
// the names after the item array in the same buffer and points at them,
// so the pointers are turned back into offsets instead of dereferenced.
func pdhValues(counter uintptr) map[string]float64 {
	if counter == 0 {
		return nil
	}
	var size, count uint32
	r, _, _ := procPdhGetArray.Call(counter, pdhFmtDouble|pdhNoCap100, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if r != pdhMoreData || size == 0 || size > 64<<20 {
		return nil
	}
	buf := make([]byte, size)
	if r, _, _ := procPdhGetArray.Call(counter, pdhFmtDouble|pdhNoCap100, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0]))); r != 0 {
		return nil
	}
	base := uintptr(unsafe.Pointer(&buf[0]))
	out := make(map[string]float64, count)
	for i := 0; i < int(count) && (i+1)*pdhItemSize <= len(buf); i++ {
		it := buf[i*pdhItemSize:]
		off := int(uintptr(le64(it)) - base)
		if off < 0 || off >= len(buf) || le32(it[8:]) > 1 { // 0 valid, 1 new data
			continue
		}
		out[utf16Bytes(buf[off:])] = math.Float64frombits(le64(it[16:]))
	}
	return out
}

// instLUID extracts "0x00000000_0x0000abcd" from a PDH instance name.
func instLUID(name string) (string, bool) {
	i := strings.Index(name, "luid_")
	if i < 0 || len(name) < i+5+21 {
		return "", false
	}
	return strings.ToLower(name[i+5 : i+5+21]), true
}

func (p *platform) sampleGPU(s *Sample, info *Info) {
	g := &p.gpu
	if g.dead.Load() {
		return
	}
	now := time.Now()
	// A rate counter averages over the whole gap since its previous
	// collect: after the page sat hidden, take one reading to re-baseline
	// and report nothing rather than a long-run average.
	stale := !g.last.IsZero() && now.Sub(g.last) > 2500*time.Millisecond
	g.last = now
	if g.cur == nil {
		if g.cur = g.ready.Swap(nil); g.cur == nil {
			g.startOpen()
			return
		}
	}
	g.cur.collect()
	switch {
	case g.next != nil:
		g.next.collect()
		if g.next.collects >= 2 {
			g.cur.close()
			g.cur, g.next = g.next, nil
		}
	case now.Sub(g.cur.born) > gpuQueryLife:
		if g.next = g.ready.Swap(nil); g.next != nil {
			g.next.collect()
		} else {
			g.startOpen()
		}
	}
	if stale || g.cur.collects < 2 {
		return
	}
	engines := map[string]map[string]float64{} // luid → engine type → % summed over processes
	utilVals := pdhValues(g.cur.util)
	for name, v := range utilVals {
		luid, ok := instLUID(name)
		if !ok {
			continue
		}
		typ := "Other"
		if i := strings.Index(name, "engtype_"); i >= 0 && i+len("engtype_") < len(name) {
			typ = name[i+len("engtype_"):]
		}
		if engines[luid] == nil {
			engines[luid] = map[string]float64{}
		}
		engines[luid][typ] += v
	}
	ded, shr := map[string]float64{}, map[string]float64{}
	dedVals := pdhValues(g.cur.dedicated)
	for name, v := range dedVals {
		if luid, ok := instLUID(name); ok {
			ded[luid] += v
		}
	}
	for name, v := range pdhValues(g.cur.shr) {
		if luid, ok := instLUID(name); ok {
			shr[luid] += v
		}
	}
	s.GPUs = make([]GPUSample, len(info.GPUs))
	for i, gi := range info.GPUs {
		gs := &s.GPUs[i]
		// PDH lists only instances that exist — an idle adapter simply has
		// none, so "the query works" decides availability and absence is 0.
		gs.HasUtilization = len(utilVals) > 0
		gs.HasMemory = len(dedVals) > 0
		gs.Engines = map[string]float64{}
		var dedUsed, shrUsed float64
		for _, luid := range gi.luids {
			luid = strings.ToLower(luid)
			for typ, v := range engines[luid] {
				gs.Engines[typ] += v
			}
			dedUsed += ded[luid]
			shrUsed += shr[luid]
		}
		for typ, v := range gs.Engines {
			v = clampPct(v)
			gs.Engines[typ] = v
			gs.Util = max(gs.Util, v)
		}
		gs.DedicatedUsed, gs.SharedUsed = uint64(dedUsed), uint64(shrUsed)
	}
}
