package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/dumpingground/dumpingground/internal/disk"
	"log"
	"os"
	"time"
)

func snapshotSummary(s *disk.Snapshot) map[string]uint64 {
	if s == nil {
		return nil
	}
	return map[string]uint64{
		"files": s.Files, "dirs": s.Dirs, "bytes": s.Bytes, "allocated": s.Allocated,
		"skipped": s.Skipped, "estimated": s.Estimated,
		"attrFiltered": s.AttrFiltered, "unlinked": s.Unlinked,
	}
}

func main() {
	root := flag.String("volume", "C:\\", "NTFS volume root, e.g. C:\\")
	timeout := flag.Duration("timeout", 10*time.Minute, "verification timeout")
	jsonOut := flag.Bool("json", false, "write JSON")
	mftOnly := flag.Bool("mft-only", false, "probe the strict MFT path without the reference walk")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *mftOnly {
		snap, err := disk.ProbeMFT(ctx, *root)
		if err != nil {
			log.Fatalf("strict MFT read failed: %v", err)
		}
		out, _ := json.MarshalIndent(map[string]any{"duration": snap.Duration.String(), "snapshot": snapshotSummary(snap)}, "", "  ")
		fmt.Println(string(out))
		return
	}
	report, err := disk.VerifyMFT(ctx, *root)
	if err != nil {
		log.Fatal(err)
	}
	if *jsonOut {
		slim := struct {
			Status       disk.VerifyStatus       `json:"status"`
			Root         string                  `json:"root"`
			MFTDuration  time.Duration           `json:"mftDuration"`
			WalkDuration time.Duration           `json:"walkDuration"`
			Error        string                  `json:"error"`
			MetadataOnly int                     `json:"metadataOnly"`
			MFT          map[string]uint64       `json:"mft,omitempty"`
			Walk         map[string]uint64       `json:"walk,omitempty"`
			Differences  []disk.VerifyDifference `json:"differences"`
		}{Status: report.Status, Root: report.Root, MFTDuration: report.MFTDuration, WalkDuration: report.WalkDuration, Error: report.Error, MetadataOnly: report.MetadataOnly, Differences: report.Differences}
		slim.MFT = snapshotSummary(report.MFT)
		slim.Walk = snapshotSummary(report.Walk)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(slim)
	} else {
		fmt.Printf("status=%s volume=%s mft=%s walk=%s differences=%d metadataOnly=%d\n", report.Status, report.Root, report.MFTDuration, report.WalkDuration, len(report.Differences), report.MetadataOnly)
		if report.Error != "" {
			fmt.Println("error:", report.Error)
		}
		for i, d := range report.Differences {
			if i >= 20 {
				fmt.Printf("... %d more\n", len(report.Differences)-i)
				break
			}
			fmt.Printf("%s: %s  mft=%d/%d walk=%d/%d\n", d.Kind, d.Path, d.MFTLogical, d.MFTAllocated, d.WalkLogical, d.WalkAllocated)
		}
	}
	if report.Status != disk.VerifyPass {
		os.Exit(2)
	}
}
