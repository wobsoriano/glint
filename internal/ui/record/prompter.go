package record

import (
	"math"
	"strings"

	"github.com/egoist/mygo"
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
	prompterInk    = ui.RGBA(0x0B, 0x0B, 0x0D, 0.95)
	prompterText   = ui.RGB(0xF4, 0xF4, 0xF6)
	prompterNote   = ui.RGBA(255, 255, 255, 0.6)
	prompterListen = ui.RGB(0x34, 0xC7, 0x59)
)

// prompter is the teleprompter: the script under the top edge of the recorded display,
// a few lines at a time. With a follower it keeps the line being read at the top; the wheel
// moves away from that until the narration reaches the next word.
type prompter struct {
	words   []string
	lines   []span
	visible int
	follow  *follower
	// current is the index of the script word last read, or -1.
	current int
	// manual is the wheel's offset, in lines, from where following puts the first line.
	manual int
	wheel  float32
}

func newPrompter(script string, visible int, follow *follower) *prompter {
	return &prompter{words: strings.Fields(script), visible: visible, follow: follow, current: -1}
}

func prompterHeight(visible int) float64 {
	return prompterPadY*2 + float64(visible*prompterLineH) + prompterStatusH
}

func (c *Controller) showPrompter(t Target, pr *prompter) {
	c.closeWindow(&c.prompter)
	d := t.Display
	pw, ph := scaled(prompterW, d.Scale), scaled(prompterHeight(pr.visible), d.Scale)
	r := platform.Rect{X: d.Bounds.X + (d.Bounds.W-pw)/2, Y: d.Bounds.Y, W: pw, H: ph}
	win := c.openChrome(duringRecording, r, d.Scale, func(ctx *ui.Context) { c.prompterView(ctx, pr) })
	if pr.follow != nil {
		pr.follow.onChange(win.Invalidate)
		win.OnClosed(pr.follow.stop)
	}
	c.prompter, c.prompt = win, pr
}

// topLine is the first line shown while word active (-1 for none yet) is the last read: its
// line, or the next once the line's last word is read, never past the last full page.
func topLine(lines []span, active, visible int) int {
	if active < 0 {
		return 0
	}
	line := 0
	for i, l := range lines {
		if active >= l.start && active < l.end {
			line = i
			if l.end == active+1 {
				line++
			}
			break
		}
	}
	return min(line, max(len(lines)-visible, 0))
}

// first is the first line shown.
func (pr *prompter) first() int {
	return min(max(topLine(pr.lines, pr.current, pr.visible)+pr.manual, 0), max(len(pr.lines)-pr.visible, 0))
}

// read moves to the word last read; a new word drops the wheel's offset.
func (pr *prompter) read(current int) {
	if current != pr.current {
		pr.current = current
		pr.manual = 0
	}
}

// scroll moves the first line shown by whole lines as the wheel turns, within the script.
func (pr *prompter) scroll(dy float32) {
	pr.wheel += dy
	steps := float32(math.Trunc(float64(pr.wheel / prompterLineH)))
	pr.wheel -= steps * prompterLineH
	pr.manual += int(steps)
	pr.manual = pr.first() - topLine(pr.lines, pr.current, pr.visible)
}

// status is what the status row says, whether the follower is listening, and why it
// failed.
func (pr *prompter) status() (note string, listening bool, why string) {
	if pr.follow == nil {
		return "Scroll to move through the script", false, ""
	}
	s, current := pr.follow.state()
	pr.read(current)
	return s.note(), s.stage == followListening, s.err
}

func (c *Controller) prompterView(ctx *ui.Context, pr *prompter) {
	p := theme.Apply(ctx)
	ctx.Root().Background(ui.Transparent)
	if pr.lines == nil {
		widths := make([]float32, len(pr.words))
		for i, w := range pr.words {
			widths[i], _ = ctx.MeasureText(0, ui.Span{Text: w, Font: theme.Font(), Size: prompterFont, Weight: 500})
		}
		pr.lines = wrap(widths, prompterWordGap, prompterW-prompterPadX*2-4)
	}
	note, listening, why := pr.status()
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
			top := pr.first()
			for _, l := range pr.lines[top:min(top+pr.visible, len(pr.lines))] {
				ui.Row(ctx).Height(prompterLineH).Gap(prompterWordGap).AlignItems(ui.Center).Children(func() {
					for i := l.start; i < l.end; i++ {
						word := ui.Text(ctx, pr.words[i]).FontSize(prompterFont).FontWeight(500).TextColor(prompterText).SingleLine()
						if i <= pr.current {
							word.TextColor(p.Accent)
						}
						if i == pr.current {
							word.Background(p.Accent.Alpha(0.25)).Radius(3)
						}
					}
				})
			}
		})
		dot := prompterNote
		if listening {
			dot = prompterListen
		}
		status := ui.Row(ctx).Height(prompterStatusH).Gap(6).AlignItems(ui.Center)
		if why != "" {
			status.Tooltip(why)
		}
		status.Children(func() {
			ui.Box(ctx).Size(6, 6).Radius(3).Background(dot)
			ui.Text(ctx, note).FontSize(theme.Caption).TextColor(prompterNote).SingleLine()
		})
	})
	c.probe(SurfacePrompter, "text", root)
}

// PrompterPosition is the index of the script word the teleprompter last heard, -1 before
// the first or without a teleprompter, and what its status row says, for drivers.
func (c *Controller) PrompterPosition() (index int, note string) {
	index = -1
	mygo.RunOnMain(func() {
		if c.prompt != nil && c.prompter != nil {
			note, _, _ = c.prompt.status()
			index = c.prompt.current
		}
	})
	return index, note
}
