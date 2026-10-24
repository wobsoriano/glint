package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Glint",
			Width:   480,
			Height:  320,
			Content: ui.View(func(c *ui.Context) { ui.Column(c).Fill().Center().Children(func() { ui.Text(c, "Glint") }) }),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
