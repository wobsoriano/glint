package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// chrome is a Library-like window in Glint's Windows 11 look: Mica behind a hidden title bar with
// the system's caption buttons, a sidebar over the material, and opaque content.
type chrome struct {
	nav      string
	bar      ui.TitleBar
	vibrancy bool
	accent   ui.Color
}

func (a *chrome) view(c *ui.Context) {
	p := theme.Apply(c)
	a.bar, a.vibrancy, a.accent = c.TitleBar(), c.Vibrancy(), c.Preferences().Accent
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	bar := c.TitleBar()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(max(bar.Height, 40)).Padding(0, bar.Right+12, 0, bar.Left+14).Gap(10).DragWindow().Children(func() {
			ui.Image(c, theme.Mark(false)).Size(18, 18)
			ui.Text(c, "Glint Library").FontSize(theme.Caption)
		})
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			side := ui.Column(c).Width(220).Padding(4, 8).Gap(2)
			if !c.Vibrancy() {
				side.Background(p.Layer)
			}
			side.Children(func() {
				for _, it := range []struct {
					icon  theme.IconName
					label string
				}{{theme.IconLibrary, "All captures"}, {theme.IconImage, "Screenshots"}, {theme.IconFilm, "Recordings"}, {theme.IconPin, "Pinned"}, {theme.IconTrash, "Trash"}} {
					row := ui.Row(c.Key(it.label)).Height(36).Padding(0, 12).Gap(12).Radius(theme.RadiusControl)
					on := a.nav == it.label
					if on {
						row.Background(p.ControlHover)
					} else if row.Hovered() {
						row.Background(p.Control)
					}
					row.Children(func() {
						if on {
							ui.Box(c).Absolute().Left(0).Top(10).Size(3, 16).Radius(2).Background(p.Accent)
						}
						ui.Icon(c, theme.Icon(it.icon)).Size(16, 16)
						ui.Text(c, it.label)
					})
					if row.Clicked() {
						a.nav = it.label
					}
				}
			})
			ui.Column(c).Grow(1).Background(p.Base).Radius(theme.RadiusWindow).Border(1, p.Border).Padding(20).Gap(16).Margin(0, 8, 8, 0).Children(func() {
				ui.Text(c, a.nav).FontSize(theme.Subtitle).FontWeight(theme.Semibold)
				ui.Row(c).Gap(8).Children(func() {
					ui.PrimaryButton(c, "New capture")
					ui.Button(c, "Import")
					ui.Spacer(c)
					ui.Text(c, fmt.Sprintf("accent %02X%02X%02X, Mica %v", a.accent.R, a.accent.G, a.accent.B, c.Vibrancy())).TextColor(p.Muted).FontSize(theme.Caption)
				})
				ui.Row(c).Gap(12).Wrap().Children(func() {
					for i := 0; i < 6; i++ {
						ui.Box(c.Key(i)).Size(180, 112).Radius(theme.RadiusWindow).Background(p.Card).Border(1, p.Border).
							Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.12))
					}
				})
				ui.Text(c, "Segoe UI Variable Text, 14 px body: The quick brown fox jumps over the lazy dog.")
			})
		})
	})
}

func runChrome() {
	mygo.App.WhenReady(func() {
		a := &chrome{nav: "All captures"}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "Glint Library", Width: 1000, Height: 640, X: 120, Y: 80,
			TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancyMica,
			Content: ui.View(a.view),
		})
		if *auto {
			go driveChrome(a, win)
		}
	})
}

func driveChrome(a *chrome, win *mygo.Window) {
	failures := 0
	defer finish(&failures)
	time.Sleep(800 * time.Millisecond)
	hwnd := win.NativeHandle()
	pSetForegroundWindow.Call(hwnd)
	prefs := mygo.Theme.Preferences()
	logf("system: dark %v, accent %v, reduce transparency %v, text scale %.2f", mygo.Theme.IsDark(), prefs.Accent, prefs.ReduceTransparency, prefs.TextScale)
	var bar ui.TitleBar
	var vib bool
	mygo.RunOnMain(func() { bar, vib = a.bar, a.vibrancy })
	check(&failures, vib, "the window shows Mica behind the view (c.Vibrancy)")
	check(&failures, bar.Right > 100 && bar.Height > 0, "the hidden title bar keeps the system's caption buttons: %+v", bar)
	check(&failures, prefs.Accent.A != 0, "the desktop reports an accent: %v", prefs.Accent)
	for _, src := range []mygo.ThemeSource{mygo.ThemeDark, mygo.ThemeLight, mygo.ThemeSystem} {
		mygo.RunOnMain(func() { mygo.Theme.SetSource(src) })
		time.Sleep(600 * time.Millisecond)
		wr := frameRect(hwnd)
		shot := captureScreen(wr)
		savePNG(out("chrome-"+string(src)+".png"), shot)
		cap := shot.RGBAAt(int(wr.w())-20, 15)
		logf("theme %s: caption area pixel %v, title bar pixel %v", src, cap, shot.RGBAAt(400, 15))
	}
	fonts := map[string]int{}
	for _, f := range []string{"Segoe UI Variable Text", "Segoe UI", "Arial", "No Such Font"} {
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.RGB(255, 255, 255))
			ui.Text(c, "Glint quick brown fox 0123").Font(f).FontSize(14).TextColor(ui.RGB(0, 0, 0))
		}, 220, 24, 1)
		ink := 0
		for i := 0; i < len(img.Pix); i += 4 {
			ink += 255 - int(img.Pix[i])
		}
		fonts[f] = ink
	}
	logf("ink of the same text in each family: %v", fonts)
	check(&failures, fonts["Segoe UI Variable Text"] != fonts["No Such Font"] && fonts["Segoe UI Variable Text"] != fonts["Arial"],
		"Segoe UI Variable Text resolves to a font of its own (not the fallback)")
}
