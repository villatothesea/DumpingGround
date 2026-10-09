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

func main() {
	root := flag.String("volume", "C:\\", "NTFS volume root, e.g. C:\\")
	timeout := flag.Duration("timeout", 10*time.Minute, "verification timeout")
	jsonOut := flag.Bool("json", false, "write JSON")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := disk.VerifyMFT(ctx, *root)
	if err != nil {
		log.Fatal(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Printf("status=%s volume=%s mft=%s walk=%s differences=%d\n", report.Status, report.Root, report.MFTDuration, report.WalkDuration, len(report.Differences))
		if report.Error != "" {
			fmt.Println("error:", report.Error)
		}
		for i, d := range report.Differences {
			if i >= 20 {
				fmt.Printf("... %d more\n", len(report.Differences)-i)
				break
			}
			fmt.Printf("%s: %s\n", d.Kind, d.Path)
		}
	}
	if report.Status != disk.VerifyPass {
		os.Exit(2)
	}
}
