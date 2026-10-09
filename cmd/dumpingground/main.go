package main

import (
	"flag"
	"github.com/dumpingground/dumpingground/internal/app"
	"log"
	"os"
	"time"
)

func main() {
	// A once-per-second inspector benefits more from lower memory than from a
	// permanent D3D device. Respect explicit renderer overrides for comparison.
	if _, set := os.LookupEnv("MYGO_GPU"); !set {
		os.Setenv("MYGO_GPU", "0")
	}
	renderer := flag.String("renderer", "", "cpu or gpu; default is cpu")
	page := flag.String("page", "processes", "initial page: processes or disk")
	scan := flag.String("scan", "", "scan a folder or drive on startup")
	capture := flag.String("capture-dir", "", "write application-only diagnostic frames")
	closeAfter := flag.Duration("close-after", 0*time.Second, "close automatically after a diagnostic run")
	flag.Parse()
	if *renderer == "gpu" {
		os.Setenv("MYGO_GPU", "1")
	} else if *renderer == "cpu" {
		os.Setenv("MYGO_GPU", "0")
	}
	m := app.New()
	m.InitialPage = *page
	m.InitialScan = *scan
	m.CaptureDir = *capture
	m.AutoClose = *closeAfter
	if err := m.Run(); err != nil {
		log.Fatal(err)
	}
}
