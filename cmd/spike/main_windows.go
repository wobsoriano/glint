// Command spike holds throwaway prototypes that settle how Glint's surfaces are built with MyGo's
// native UI on Windows. Each mode opens its surface; with -auto it drives itself with synthetic
// input, logs what it measured and writes screenshots to -out, then quits. See
// docs/go-port/mygo-findings.md.
//
//	go run ./cmd/spike -mode overlay -auto
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

var (
	mode   = flag.String("mode", "overlay", "overlay, cards, protect, tray, editor, chrome, video, toast, foreground")
	auto   = flag.Bool("auto", false, "drive the prototype, measure, and quit")
	outDir = flag.String("out", "cmd/spike/evidence", "where screenshots go")
	start  = time.Now()
)

func logf(format string, args ...any) {
	fmt.Printf("[%7.1fms] %s\n", float64(time.Since(start).Microseconds())/1000, fmt.Sprintf(format, args...))
}

func out(name string) string { return filepath.Join(*outDir, name) }

// finish quits once the drive is done, with an exit code that says whether every check held.
func finish(failures *int) {
	mygo.RunOnMain(func() {
		logf("done: %d check(s) failed", *failures)
		code := 0
		if *failures > 0 {
			code = 1
		}
		mygo.App.Exit(code)
	})
}

func check(failures *int, ok bool, format string, args ...any) {
	status := "PASS"
	if !ok {
		status = "FAIL"
		*failures++
	}
	logf("%s %s", status, fmt.Sprintf(format, args...))
}

func main() {
	flag.Parse()
	modes := map[string]func(){
		"overlay":    runOverlay,
		"cards":      runCards,
		"protect":    runProtect,
		"tray":       runTray,
		"editor":     runEditor,
		"chrome":     runChrome,
		"video":      runVideo,
		"toast":      runToast,
		"foreground": runForeground,
	}
	run, ok := modes[*mode]
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown mode", *mode)
		os.Exit(2)
	}
	run()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
