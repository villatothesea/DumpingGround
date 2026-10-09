package disk

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

type VerifyStatus string

const (
	VerifyPass         VerifyStatus = "PASS"
	VerifyFail         VerifyStatus = "FAIL"
	VerifyInconclusive VerifyStatus = "INCONCLUSIVE"
)

type VerifyDifference struct {
	Path, Kind                                           string
	MFTLogical, MFTAllocated, WalkLogical, WalkAllocated uint64
}
type VerifyReport struct {
	Status                    VerifyStatus
	Root                      string
	MFT, Walk                 *Snapshot
	Differences               []VerifyDifference
	MFTDuration, WalkDuration time.Duration
	Error                     string
}

// VerifyMFT never falls back for the MFT side. It runs a second directory walk
// and compares reachable file paths and sizes. Access denial, unsupported
// records, a non-NTFS volume or an incomplete MFT result is INCONCLUSIVE.
func VerifyMFT(ctx context.Context, root string) (*VerifyReport, error) {
	report := &VerifyReport{Root: root, Status: VerifyInconclusive}
	if len(root) != 3 || root[1:] != ":\\" {
		report.Error = "verification requires a volume root"
		return report, nil
	}
	started := time.Now()
	mft, err := fastVolumeScan(ctx, root, nil)
	report.MFTDuration = time.Since(started)
	if err != nil || mft == nil {
		report.Error = fmt.Sprintf("strict MFT read failed: %v", err)
		return report, nil
	}
	report.MFT = mft
	walkStarted := time.Now()
	walk, walkErr := (&Scanner{}).scanDirectory(ctx, root, nil)
	report.WalkDuration = time.Since(walkStarted)
	if walkErr != nil {
		report.Error = fmt.Sprintf("directory reference scan failed: %v", walkErr)
		return report, nil
	}
	report.Walk = walk
	mftFiles := fileMap(mft)
	walkFiles := fileMap(walk)
	for path, m := range mftFiles {
		w, ok := walkFiles[path]
		if !ok {
			report.Differences = append(report.Differences, VerifyDifference{Path: path, Kind: "missing from directory walk", MFTLogical: m.Bytes, MFTAllocated: m.Allocated})
			continue
		}
		if m.Bytes != w.Bytes || m.Allocated != w.Allocated {
			report.Differences = append(report.Differences, VerifyDifference{Path: path, Kind: "size mismatch", MFTLogical: m.Bytes, MFTAllocated: m.Allocated, WalkLogical: w.Bytes, WalkAllocated: w.Allocated})
		}
	}
	for path := range walkFiles {
		if _, ok := mftFiles[path]; !ok {
			report.Differences = append(report.Differences, VerifyDifference{Path: path, Kind: "missing from MFT"})
		}
	}
	if len(report.Differences) > 0 {
		report.Status = VerifyFail
	} else {
		report.Status = VerifyPass
	}
	return report, nil
}
func fileMap(s *Snapshot) map[string]Node {
	out := map[string]Node{}
	if s == nil {
		return out
	}
	for i, n := range s.Nodes {
		if n.Dir {
			continue
		}
		p := filepath.Clean(s.FullPath(i))
		if p != "." {
			out[p] = n
		}
	}
	return out
}

var _ = errors.Is
