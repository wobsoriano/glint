# Building Glint's surfaces with MyGo v0.6.3 on Windows

This page records what the prototypes in `cmd/spike` showed about MyGo's native UI on Windows 11,
and how the real `internal/ui/...` packages should build each surface. Every claim below comes
from a run of `cmd/spike` unless it says "from the source" or "not verified".

## Rerun the evidence

`cmd/spike/run-all.ps1` builds the spike and runs every prototype with `-auto`. Each run drives
itself with `SendInput` for a few seconds, prints `PASS` or `FAIL` per check, and writes its log
and screenshots to `cmd/spike/evidence`. One prototype at a time:

```powershell
powershell -File cmd/spike/run.ps1 overlay -gap 800ms
powershell -File cmd/spike/run.ps1 cards -variant window   # a variant that fails on purpose
```

`go test ./cmd/spike` runs the display-scale check, which needs no window.

The machine: Windows 11 Enterprise 10.0.26200, one 1920x1080 display at 100%, 60 Hz, AMD Radeon
RX 6700 XT, dark theme, accent `#0078D4`, Go 1.27.2.

Go's clock on this machine moves in steps of about 0.5 ms (`time.Since` around a 1.5 ms loop
reads 0, 0.53, 1.07 or 1.6 ms). MyGo's `MYGO_FRAME_STATS` uses the same clock. Read every
sub-millisecond number below as "under a millisecond".

## Summary

| Surface | Build it as | Escape hatches through `NativeHandle` |
|---|---|---|
| Capture overlay | `Panel` window, one per display, created hidden at startup | `SetWindowPos` in physical pixels; `DWMWA_WINDOW_CORNER_PREFERENCE` = do not round; `DWMWA_BORDER_COLOR` = none |
| Preview cards, toasts, recording outline | `NewFlyout` with a hidden owner window | `SetWindowPos` in physical pixels |
| Editor, Library, Preferences | Normal window, `TitleBarStyle: TitleBarHidden`, `Vibrancy: VibrancyMica` | none |
| Pinned screenshots | not prototyped; a `Panel` or a flyout | not tested |

Three MyGo behaviors shape everything that floats:

1. A `Frameless` `Transparent` window keeps a redirection bitmap, so the parts the view leaves
   transparent show black. Only flyouts and windows with a `Vibrancy` are created without one.
2. Window geometry is whole DIPs, converted with the DPI the window has now, and negative
   coordinates round toward zero. Place floating windows with `SetWindowPos` in physical pixels.
3. The taskbar raises itself above a plain topmost frameless window 10 to 30 ms after it
   activates. A `Panel` window (a tool window) stays above it.

## 1. Capture overlay

**Answer: yes, with a `Panel` window and three raw calls.** The overlay covers the display
exactly, taskbar included. It draws the frozen bitmap 1:1 (0 of 2,073,600 pixels differ). It takes
the keyboard from another app's foreground window when a global shortcut shows it. Space and Esc
reach it. The cursor is the system crosshair.

Code: `newOverlay`, `(*overlay).show` (variant `panel-square`), `(*overlay).view`, `paint`,
`magnifier` in `cmd/spike/overlay_windows.go`; `placePhysical`, `setCornerPreference`,
`setBorderColor` in `cmd/spike/win32_windows.go`.

```go
o.win = mygo.NewWindow(mygo.WindowOptions{
	Title: "Glint capture", X: d.Bounds.X, Y: d.Bounds.Y, Width: d.Bounds.Width, Height: d.Bounds.Height,
	Frameless: true, AlwaysOnTop: true, SkipTaskbar: true, Hidden: true,
	DisableResize: true, DisableShadow: true, Panel: true,
	Content: ui.View(o.view),
})
hwnd := o.win.NativeHandle()

// On every show:
setCornerPreference(hwnd, 1)          // DWMWCP_DONOTROUND: Panel windows get Windows 11's round corners
setBorderColor(hwnd, 0xFFFFFFFE)      // DWMWA_COLOR_NONE: and its 1 px border
placePhysical(hwnd, monitor.bounds)   // SetWindowPos(HWND_TOPMOST, physical rect, SWP_NOACTIVATE)
o.win.Show()
o.win.Focus()
```

The view draws the bitmap over the whole box and maps the pointer back to pixels with the
painter's scale:

```go
box := ui.Box(c).Fill().Cursor(ui.CursorCrosshair).Focusable().AutoFocus()
box.HandleInput(func(ev ui.InputEvent) bool { /* InputPointerDown, Move, Up, KeyDown */ })
box.Draw(func(p *ui.Painter, r ui.Rect) {
	o.scale = p.Scale()
	p.Image(o.bmp, r, ui.FillBox) // o.bmp = ui.NewBitmap(shot).Pixelated()
	// dim: four p.Fill rects around the selection; label: p.MeasureText + p.RichText
	// magnifier: p.Clip(circle) + p.ImageRegion(o.bmp, 15x15 px around the pointer, dst, ...)
})
// physical pixel = round(dip * p.Scale()); pointer X and Y are float DIPs, so this is exact
```

### Measured

| Check | Result |
|---|---|
| Window and client rect | `(0,0 1920x1080)`, the display, taskbar strip included |
| Bitmap on screen versus source (noise image) | 0 pixels differ, no corner rounding |
| Foreground after Ctrl+Shift+4 pressed in another app's window | the overlay |
| Selection dragged from (301,203) to (941,683) | `(301,203)-(941,683)` px |
| Space, Esc | both reach the view; Esc arrives through `c.Shortcut(0, ui.KeyEscape)` before `HandleInput` |
| `Show` to first `Draw` | 6 to 15 ms, the first show of each process included; 160 ms once, in the first run on the machine (cause not found) |
| `Show` to the pixels on screen (polled with GDI) | 9 to 24 ms with a new 1920x1080 bitmap (one show of about 50 took 64 ms), 12 to 17 ms with the same one |
| Same, shown again within 200 ms of `Hide` | 45 to 62 ms |

The re-show within 200 ms is slower in every run; after an 800 ms gap it is 12 to 17 ms. The
cause is a guess (DWM's hide animation still running). Glint shows the overlay after the user
presses a key, so the 800 ms case is the one that matters.

### Window variants tried

`cmd/spike/run.ps1 overlay -variant <name>`. "Taskbar" means whether `Shell_TrayWnd` stayed above
the overlay's bottom strip at the 1:1 check, which runs about 250 ms after the overlay shows.

| Variant | Taskbar | Runs |
|---|---|---|
| `plain`: Frameless, AlwaysOnTop, SkipTaskbar, placed in pixels before `Show` | covers the overlay | 4 of 4 |
| `after`: the same, `SetWindowPos(HWND_TOPMOST)` after `Show` | covers it | 3 of 4 |
| `nocaption`: WS_CAPTION, WS_SYSMENU and WS_MINIMIZEBOX removed | covers it | 3 of 3 |
| `fullscreen`: `ITaskbarList2::MarkFullscreenWindow` | covers it | 2 of 3 |
| `nocap-mark`: both of the above | covers it | 1 of 2 |
| `notop`, `notop-nocap`: not topmost | covers it | 4 of 4 |
| `reassert`: `SetWindowPos(HWND_TOPMOST)` every 16 ms for 500 ms | overlay wins after an 80 ms flash | 2 of 2 |
| `panel`: `Panel: true` | overlay stays on top, but round corners and a border (6,068 px differ) | 2 of 2 |
| `panel-square`: `Panel` with no rounding and no border | overlay stays on top, 0 px differ | 9 of 9 |

The timeline under the taskbar for `plain` reads `0ms Shell_TrayWnd, 10ms MyGoWindow, 21ms
Shell_TrayWnd`. For `panel-square` it reads `0ms Shell_TrayWnd, 10ms MyGoWindow` and stays.

### Display scales

The machine has one display at 100%, so the GPU path was not run at another scale.
`TestScreenshotDrawsOneToOneAtEveryScale` in `cmd/spike/scale_test.go` renders a noise bitmap of
the window's pixel size over the whole box with MyGo's CPU renderer (`ui.Render`) at 1, 1.25,
1.5, 1.75, 2 and 2.25. All six are exact. The GPU renderer snaps the image rectangle to device
pixels the same way (`Painter.snap`, from the source), so 1:1 on the GPU at 150% is likely but
not verified.

Place the overlay in physical pixels, not with `WindowOptions` or `SetBounds`:

- `mygo.Display.Bounds` and `WindowOptions.X/Y/Width/Height` are whole DIPs. At 175%, a 1920 px
  display is 1097 DIPs, which converts back to 1919 px (from the source: `toPx`, `toDIP` in
  `internal/windows/win32.go`).
- `SetBounds` converts with the DPI of the monitor the window is on now, so a DIP rectangle on a
  display with another scale lands in the wrong place (from the source).
- `toPx` and `toDIP` truncate toward zero, so negative coordinates are off by one: measured in
  the cards run, `SetPosition(-264, y)` put the window at x = -263, and `Position` read -262.
  Displays left of or above the primary display have negative coordinates.

Mixed-DPI displays were not tested. When `SetWindowPos` moves a window onto a display with
another DPI, Windows sends `WM_DPICHANGED` and MyGo applies the suggested rectangle, which keeps
the DIP size (from the source). Call `placePhysical` again after the move.

## 2. Preview cards

**Answer: yes, as a flyout of a hidden owner window.** The card stack shows without taking the
keyboard. Hovering an inactive card reveals its buttons, and a click on a button runs its action
without activating the stack. Dragging a card into an Explorer window copies the PNG. A drag let go
past the screen edge over nothing reports `Canceled && !Escaped` at the edge point, which is the
dismiss gesture of the Rust build. `SetWindowPos` slides the window off-screen.

Code: `newStack`, `(*stack).card`, `(*stack).slideOut` in `cmd/spike/cards_windows.go`.

```go
owner := mygo.NewWindow(mygo.WindowOptions{Width: 1, Height: 1, Frameless: true, SkipTaskbar: true, Hidden: true})
s.win = mygo.NewFlyout(mygo.FlyoutOptions{Parent: owner, Width: w, Height: h, Hidden: true, Content: ui.View(s.view)})
placePhysical(s.win.NativeHandle(), bottomLeftOfWorkArea)
s.win.ShowInactive()

// In the view: the card is the drag source of its file.
files, _ := transfer.FileData(path) // absolute path of the saved PNG
box := ui.Box(c).Size(224, 148).Radius(theme.RadiusWindow).Clip()
box.DragData(files, transfer.DragOptions{Done: func(r transfer.Result) {
	// r.Operation == transfer.Copy: dropped; r.Canceled && !r.Escaped && r.Point at the edge: dismiss
}})
hovered := box.Hovered() // buttons inside take clicks; a press that moves starts the drag
```

| Check | `flyout` (default) | `window` | `window-noactivate` |
|---|---|---|---|
| `ShowInactive` leaves the other app foreground | yes | yes | yes |
| Space around the cards shows what is behind | yes | no, black | no, black |
| Hover reveals the buttons (29,301 px change) | yes | yes | yes |
| A click on Copy runs, and the other app stays foreground | yes | no, the stack activates | yes |
| Drag into Explorer copies `Glint spike 1.png` | yes | yes | yes |
| Drag past the left edge: `Canceled`, not `Escaped`, at `(0,840)` | yes | yes | yes |

`window` is `Frameless`, `Transparent`, `AlwaysOnTop` and `SkipTaskbar`. `window-noactivate` adds
`WS_EX_NOACTIVATE` with `SetWindowLongPtrW`. The flyout's extended style is `0x08200088`:
no-activate, no redirection bitmap, tool window and topmost.

**Slide off-screen.** Moving the window with `SetWindowPos` from a goroutine, every 7 ms with an
ease-out curve, took 29 or 30 steps in 220 ms and ended exactly at the target. Nobody watched it,
and no frame-by-frame capture was made, so smoothness is not verified. Do not use `Window.SetPosition` for it: it
rounds negative coordinates (section 1), and a flyout's own placement keeps it inside the work
area (from the source).

## 3. Exclusion from capture

**Answer: `SetContentProtection(true)` sets `WDA_EXCLUDEFROMCAPTURE` (0x11), and a GDI `BitBlt`
of the screen leaves the window out.** It falls back to `WDA_MONITOR` only when
`WDA_EXCLUDEFROMCAPTURE` fails, which is Windows before 10 2004 (from the source).

`cmd/spike/protect_windows.go` shows a magenta window over a green one and captures the screen
before and after the call. Before: magenta. After: green, the window behind, and
`GetWindowDisplayAffinity` reads 0x11. The same holds for a `Panel` window and a flyout
(`outline` run). Evidence: `cmd/spike/evidence/protect-plain.png` is all green. Windows Graphics
Capture and the Desktop Duplication API were not tested.

## 4. Tray, global shortcuts, single instance, no windows

**Answer: yes for all, with one gap: there is no double-click event.**

Code: `runTray` and `driveTray` in `cmd/spike/tray_windows.go`.

```go
tray, err := mygo.NewTray(mygo.TrayOptions{ID: "glint", Icon: markPNG(32), ToolTip: "Glint", Menu: mygo.NewMenu([]*mygo.MenuItem{
	{Label: "Capture Area", Accelerator: "Ctrl+Shift+4", Click: captureArea},
	{Label: "After capture", Submenu: []*mygo.MenuItem{
		{Label: "Show preview", Type: mygo.MenuItemCheckbox, Checked: true, Click: toggle},
		{Label: "PNG", Type: mygo.MenuItemRadio, Checked: true},
		{Label: "JPEG", Type: mygo.MenuItemRadio},
	}},
	mygo.Separator(),
	{Role: mygo.RoleQuit},
})})
tray.OnClick(onClick) // the left button; a menu set on the tray opens on the right button

if !mygo.App.RequestSingleInstanceLock() { return } // in main, before App.Run
mygo.App.OnSecondInstance(func(args []string, wd string) { /* forwarded arguments */ })
mygo.App.OnWindowAllClosed(func() {})               // keep running with no window
```

| Check | Result |
|---|---|
| App runs with no window | `len(mygo.Windows()) == 0` while the tray and shortcuts work |
| `Register("Ctrl+Shift+4")`, then the keys | callback runs |
| The same shortcut twice | `mygo: shortcut Ctrl+Shift+4 is already registered` |
| A shortcut another process holds | `mygo: cannot register "Ctrl+Shift+F9": Hot key is already registered.` |
| `"Ctrl+Shift+Bogus"` | `accelerator: unknown key "Bogus" in "Ctrl+Shift+Bogus"` |
| `Unregister`, then the keys | nothing runs; `IsRegistered` is false |
| A second instance with `-- --capture area` | the first gets `["-mode" "tray" "--" "--capture" "area"]` and the working directory; the second exits |
| Left click | `OnClick` |
| Double click | two `OnClick` calls 71 to 81 ms apart; no double-click event |
| Right click | the menu opens; a checkbox in the submenu flips `Checked` before `Click` runs |

The Rust build captures an area on a tray double-click. MyGo reports the button-up of each click
only (`WM_LBUTTONUP`, from the source). To keep the behavior, time two `OnClick` calls against
`GetDoubleClickTime` (500 ms here). A single-click action then waits out that time.

A new icon lands in the Windows 11 overflow flyout. `Tray.Bounds` then returns the chevron's
rectangle (`{X:1706 Y:1032 Width:32 Height:48}`), not the icon's. The test found the icon by its
orange pixels in the open overflow.

## 5. Editor canvas

**Answer: yes, all three ways hold 60 frames a second while an annotation is dragged.** The display
refresh rate (60 Hz) is the limit in each case. The scene is a 2560x1440 capture at zoom 0.5 with
50 annotations (rectangles, arrows, ellipses, highlights and text) and eight selection handles. The
pointer moves every 4 ms for 2.5 s.

Code: `editor`, `paint`, `compose`, `drawAnn` in `cmd/spike/editor_windows.go`. `-variant vector`
draws everything with the painter each frame. `cpu` composites an `*image.RGBA` on the CPU each
drag frame and pushes it with `Bitmap.Update`. `canvas` redraws a `ui.Canvas` when the document
changes and shows its bitmap.

```go
view := ui.Scaling(zoom, zoom).Then(ui.Translation(panX, panY)) // scale first, then pan
p.Transform(view, r.X, r.Y, func() {
	p.Image(captureBmp, ui.Rect{X: r.X, Y: r.Y, W: 2560, H: 1440}, ui.FillBox)
	for _, a := range anns { drawAnn(p, a) } // boxes offset by r.X, r.Y
})
// pointer to document: (ev.X - panX) / zoom
```

| Variant | Draw calls a second | Draw interval p50 / p90 / max (ms) | CPU frame p50 / p90 (ms) | Upload and present p50 / p90 (ms) |
|---|---|---|---|---|
| `vector` | 60 | 16.7 / 17.2 / 23.2 | under 1 / under 1 | under 1 / under 1 |
| `canvas` | 61 | 16.7 / 17.4 / 20.5 | under 1 / 1.1 | under 1 / 1.0 |
| `cpu` | 60 | 16.7 / 17.3 / 18.9 | 11.8 / 12.6 | 1.0 / 3.6 |

In the `cpu` variant, compositing takes 10.4 ms p50, and copying the 2560x1440 capture is 1.4 ms of
that. The rest is `golang.org/x/image/vector` filling the annotations' bounding boxes. That leaves
about 5 ms of a 16.7 ms frame. A 120 Hz display or a costlier compositor would drop frames.

GPU time is not in these numbers: MyGo's frame stats stop at `Present`. The steady 16.7 ms
interval shows the GPU keeps up at 60 Hz. During the drag, MyGo redrew only the damaged part in
111 of 150 `vector` frames ("drawn on the GPU, damaged part only"). Continuous zooming, which
redraws everything, was not timed.

Recommendation: draw the editor with the painter (`vector`) and use `internal/render` only for
export and copy. Use a `ui.Canvas` for layers that change seldom, such as a blur or pixelate
preview.

## 6. Windows 11 look

**Answer: close to `docs/design.md`.** Mica shows behind the view. The hidden title bar keeps the
system's minimize, maximize and close buttons. The desktop accent arrives through
`c.Preferences().Accent`. The window follows `Theme.SetSource` and the system theme. Segoe UI
Variable resolves.

Code: `chrome` in `cmd/spike/chrome_windows.go`. Evidence: `cmd/spike/evidence/chrome-dark.png`,
`chrome-light.png`, `chrome-system.png`.

```go
mygo.NewWindow(mygo.WindowOptions{
	Title: "Glint Library", Width: 1000, Height: 640,
	TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancyMica,
	Content: ui.View(view),
})

func view(c *ui.Context) {
	p := theme.Apply(c) // Glint's palette and type on MyGo's widgets
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent) // Mica shows where nothing is drawn
	}
	bar := c.TitleBar() // {Height:32 Left:0 Right:138}: room for the caption buttons
	ui.Row(c).Height(max(bar.Height, 40)).Padding(0, bar.Right+12, 0, bar.Left+14).DragWindow().Children(...)
	// sidebar with no background over Mica; content panel with p.Base, theme.RadiusWindow, 1 px p.Border
}
```

| Check | Result |
|---|---|
| `c.Vibrancy()` | true |
| `c.TitleBar()` | `{Height:32 Left:0 Right:138}` |
| `Theme.Preferences()` | dark, accent `{0 120 212 255}`, text scale 1, transparency on |
| `Theme.SetSource(ThemeDark)`, `ThemeLight`, `ThemeSystem` | the title bar area reads `(26,34,38)`, `(236,244,248)`, `(26,34,38)` |
| `Font("Segoe UI Variable Text")` | ink 93,688, against 94,735 for both `Segoe UI` and an unknown family (the fallback is Segoe UI) |

Not checked: the snap layouts flyout on the maximize button, title bar double-click, Alt+Space,
and keyboard focus in the caption buttons.

## 7. Video preview

**Answer: yes.** A 1920x1080 `ui.Bitmap` updated from a producer goroutine drew 30.0 frames a
second at 30 fps and 60.0 at 60 fps over 3 s each: one `Draw` for each update (90 and 180).

Code: `runVideo` in `cmd/spike/video_windows.go`.

```go
ring := make([]*image.RGBA, 4) // Update takes an *image.RGBA without a copy: do not reuse it at once
bmp := ui.NewBitmap(ring[0])
go func() {
	for n := 1; ; n++ {
		img := ring[n%len(ring)]
		decodeInto(img)
		win.Update(func() { bmp.Update(img) }) // same size: the texture stays and the pixels upload
	}
}()
// view: ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) { p.Image(bmp, r, ui.Contain) })
```

Writing a frame of synthetic pixels takes about 1.5 ms (1.1 to 2.6 ms in a run outside the
window). A real decoder costs more, and
upload cost was not separated from present here. The `cpu` editor run, which uploads a
2560x1440 bitmap each frame, measured present at 1.0 ms p50 and 3.6 ms p90.

## 8. Toasts

**Answer: yes, as a flyout of a hidden owner, faded with `SetOpacity`.** The toast shows near the
bottom centre of the work area without taking the keyboard. The corners around the pill show what
is behind it. 34 to 36 `SetOpacity` steps in 300 ms fade it out, and a capture mid-fade reads a
blend of the toast and what is behind it.

Code: `showToast`, `(*toast).fade` in `cmd/spike/toast_windows.go`.

```go
owner := mygo.NewWindow(mygo.WindowOptions{Width: 1, Height: 1, Frameless: true, SkipTaskbar: true, Hidden: true})
win := mygo.NewFlyout(mygo.FlyoutOptions{Parent: owner, Width: 320, Height: 50, Hidden: true, Content: ui.View(pill)})
placePhysical(win.NativeHandle(), bottomCentreOfWorkArea)
win.ShowInactive()
// later, from a goroutine:
for t := 0.0; t < 1; t = elapsed() { win.SetOpacity(1 - t); time.Sleep(8 * time.Millisecond) }
win.Destroy(); owner.Destroy()
```

`SetOpacity` makes the window layered (`WS_EX_LAYERED` with `LWA_ALPHA`, from the source).

## Recording outline

The brief did not ask about the recording UI, but its dashed outline uses the same parts, so
`cmd/spike/outline_windows.go` checks it. A flyout of a hidden owner with a dashed 2 px border,
`SetIgnoreMouseEvents(true)` and `SetContentProtection(true)`:

- draws the dashes (278 of 608 top-edge pixels red),
- shows the app behind through its middle (green; a plain `Transparent` window shows black there),
- passes clicks through, on the border too (`WindowFromPoint` returns the app behind), and
- leaves the screen capture with protection on (0 red pixels).

## 9. What blocks Glint, and the way around it

Nothing blocks a feature outright. These gaps need the workaround named:

| Gap | Workaround, all verified here unless marked |
|---|---|
| A `Transparent` window without a material shows black where the view draws nothing | `NewFlyout` with a hidden owner window |
| The taskbar covers a plain topmost frameless window | `Panel: true`, then `DWMWA_WINDOW_CORNER_PREFERENCE` = 1 and `DWMWA_BORDER_COLOR` = `0xFFFFFFFE` |
| Window geometry is whole DIPs, converted with the window's current DPI, rounded toward zero | `SetWindowPos` in physical pixels through `NativeHandle`; again after `WM_DPICHANGED` on mixed-DPI setups (not verified) |
| No tray double-click event | time two `OnClick` calls against `GetDoubleClickTime` |
| `Tray.Bounds` is the overflow chevron when the icon is in the overflow | none needed by Glint; do not anchor UI to it |
| A click on a plain floating window activates it | a flyout (`WS_EX_NOACTIVATE`), or add `WS_EX_NOACTIVATE` with `SetWindowLongPtrW` |
| Esc reaches `c.Shortcut` before `HandleInput` | handle Esc with `c.Shortcut(0, ui.KeyEscape)` |
| A window shown again within 200 ms of `Hide` takes 45 to 62 ms to appear | keep the overlay hidden between uses; shows after a longer gap take 12 to 24 ms |

Run every raw call on the main thread (`mygo.RunOnMain`), as MyGo's docs require. The spike calls
some from goroutines, which worked here.

## Not verified

- Any display scale other than 100% on the GPU, more than one display, mixed DPI, and displays
  at negative coordinates. The overlay code creates one window per monitor, but ran with one.
- Showing the overlay from a tray click or a menu item instead of a global shortcut.
- macOS. The spike builds for darwin only as a stub `main`.
- Windows Graphics Capture and Desktop Duplication against content protection.
- Hit-testing in the transparent parts of a flyout that does not ignore the mouse: clicks there
  may go to the flyout.
- A pinned-screenshot window, the recording control bar, Studio and the trim view.
- The tray submenu on screen: the keyboard opened it and its checkbox toggled, but the screenshot
  shows only the top menu.
- GPU time per frame, and continuous zoom in the editor.

## The theme package

`internal/ui/theme` holds Glint's look for every UI package:

- `theme.New(dark bool, accent ui.Color, backdrop bool) Palette` is the Rust build's `read_theme`:
  surfaces translucent over Mica and opaque without it, the Windows default blue when the desktop
  has no accent, a dim accent lifted a third toward white in the dark, and black text on a light
  accent.
- `theme.Apply(c)` sets the palette, Segoe UI Variable Text (the system font on macOS), 14 px body
  text (13 on macOS) and 4 px control radius on MyGo's widgets, and returns the `Palette`.
  `theme.For(c)` returns the palette alone.
- `RadiusWindow` (8), `RadiusControl` (4), `RadiusPill`, `Spacing` (4), and the type sizes
  `Caption`, `Body`, `BodyMac`, `Subtitle`, `Title` and `Semibold`.
- `theme.Icon(theme.IconCopy)` returns the parsed SVG for `ui.Icon` and `Painter.Icon`.
  `IconName` has one constant for each of the 71 icons in `src/assets.rs`, and
  `IconByName("save-as")` finds one by its Rust name. `go generate ./internal/ui/theme`
  regenerates `icons_gen.go` from `src/assets.rs`.
- `theme.Mark(template bool)` is the Glare mark as an SVG for `ui.Image`, and `MarkSource` its
  text. Rendered at 1024 px, it differs from `assets/glint-1024.png` by 0.07 of 255 per channel on
  average.

`go test ./internal/ui/theme` checks that every icon and both marks parse with `ui.ParseSVG`, that
the icons equal `src/assets.rs`, that every icon draws ink, that the mark matches the PNG, the
palette rules, and that `Apply` follows the window between dark and light.
