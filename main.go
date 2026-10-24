// Glint is a screenshot, annotation and screen-recording app that lives in the tray.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/app"
)

func main() {
	cwd, _ := os.Getwd()
	launch, err := app.ParseArgs(os.Args[1:], cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	mygo.App.SetName(app.InstanceName())
	// A second Glint hands its command line to the first and exits.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	if err := app.New(launch).Run(); err != nil {
		log.Fatal(err)
	}
}
