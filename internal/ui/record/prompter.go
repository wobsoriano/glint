package record

import (
	"math"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	prompterW                  = 560
	prompterPadX, prompterPadY = 22, 10
	prompterLineH              = 26
	prompterWordGap            = 5
	prompterFont               = 16
	prompterStatusH            = 22
)

var (
	prompterInk  = ui.RGBA(0x0B, 0x0B, 0x0D, 0.95)
	prompterText = ui.RGB(0xF4, 0xF4, 0xF6)
	prompterNote = ui.RGBA(255, 255, 255, 0.6)
)

// prompter is the teleprompter: the script under the top edge of the recorded display,
// a few lines at a time. The wheel moves through it. It does not follow the narration:
// internal/transcribe has no live recognizer to feed its ScriptTracker.
type prompter struct {
	words   []string
	lines   []span
	visible int
	top     int
	wheel   float32
}

func prompterHeight(visible int) float64 {
	return prompterPadY*2 + float64(visible*prompterLineH) + prompterStatusH
}

func (c *Controller) showPrompter(t Target, script string, visible int) {
	c.closeWindow(&c.prompter)
	d := t.Display
	pw, ph := scaled(prompterW, d.Scale), scaled(prompterHeight(visible), d.Scale)
	r := platform.Rect{X: d.Bounds.X + (d.Bounds.W-pw)/2, Y: d.Bounds.Y, W: pw, H: ph}
	pr := &prompter{words: strings.Fields(script), visible: visible}
	c.prompter = c.openChrome(duringRecording, r, d.Scale, func(ctx *ui.Context) { c.prompterView(ctx, pr) })
}

// scroll moves the first line shown by whole lines as the wheel turns, within the script.
func (pr *prompter) scroll(dy float32) {
	pr.wheel += dy
	steps := float32(math.Trunc(float64(pr.wheel / prompterLineH)))
	pr.wheel -= steps * prompterLineH
	pr.top = min(max(pr.top+int(steps), 0), max(len(pr.lines)-pr.visible, 0))
}

func (c *Controller) prompterView(ctx *ui.Context, pr *prompter) {
	theme.Apply(ctx)
	ctx.Root().Background(ui.Transparent)
	if pr.lines == nil {
		widths := make([]float32, len(pr.words))
		for i, w := range pr.words {
			widths[i], _ = ctx.MeasureText(0, ui.Span{Text: w, Font: theme.Font(), Size: prompterFont, Weight: 500})
		}
		pr.lines = wrap(widths, prompterWordGap, prompterW-prompterPadX*2-4)
	}
	root := ui.Column(ctx).Fill().PaddingX(prompterPadX).PaddingY(prompterPadY).Radius(0, 0, 16, 16).
		Background(prompterInk).Border(1, ui.RGBA(255, 255, 255, 0.15))
	root.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		pr.scroll(ev.DY)
		return true
	})
	root.Children(func() {
		ui.Column(ctx).Height(float32(pr.visible * prompterLineH)).Clip().Children(func() {
			end := min(pr.top+pr.visible, len(pr.lines))
			for _, l := range pr.lines[pr.top:end] {
				ui.Row(ctx).Height(prompterLineH).Gap(prompterWordGap).AlignItems(ui.Center).Children(func() {
					for _, w := range pr.words[l.start:l.end] {
						ui.Text(ctx, w).FontSize(prompterFont).FontWeight(500).TextColor(prompterText).SingleLine()
					}
				})
			}
		})
		ui.Row(ctx).Height(prompterStatusH).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(ctx).Size(6, 6).Radius(3).Background(prompterNote)
			ui.Text(ctx, "Scroll to move through the script").FontSize(theme.Caption).TextColor(prompterNote).SingleLine()
		})
	})
	c.probe(SurfacePrompter, "text", root)
}
