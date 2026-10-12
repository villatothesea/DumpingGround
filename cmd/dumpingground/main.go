package main

import (
	"flag"
	"log"
	"os"
	"runtime/debug"
	"time"

	"github.com/dumpingground/dumpingground/internal/app"
	"github.com/dumpingground/dumpingground/internal/elevate"
	"github.com/dumpingground/dumpingground/internal/elevwork"
)

func main() {
	// The disk index holds on the order of a million nodes; do not let the
	// heap idle at twice the live set before the collector wakes up.
	debug.SetGCPercent(50)
	// A once-per-second inspector benefits more from lower memory than from a
	// permanent D3D device. Respect explicit renderer overrides for comparison.
	if _, set := os.LookupEnv("MYGO_GPU"); !set {
		os.Setenv("MYGO_GPU", "0")
	}
	renderer := flag.String("renderer", "", "cpu or gpu; default is cpu")
	page := flag.String("page", "computer", "initial page: computer, processes or disk")
	scan := flag.String("scan", "", "scan a folder or drive on startup")
	capture := flag.String("capture-dir", "", "write application-only diagnostic frames")
	closeAfter := flag.Duration("close-after", 0*time.Second, "close automatically after a diagnostic run")
	elevWorker := flag.Bool("elevated-worker", false, "run as the parent's privileged executor (internal)")
	elevParent := flag.Uint("elev-parent", 0, "parent pid for --elevated-worker")
	elevPipe := flag.String("elev-pipe", "", "pipe name for --elevated-worker")
	flag.Parse()
	if *elevWorker {
		// No window, no GUI loop — answer the parent's pipe and die
		// with it. Elevated via the runas prompt that spawned us.
		os.Exit(elevwork.Serve(uint32(*elevParent), *elevPipe))
	}
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
	elevate.Shutdown() // the parent-death watch would kill it anyway
}
