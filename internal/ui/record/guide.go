package record

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

var shade = ui.RGBA(0, 0, 0, 0.5)

// showGuide outlines the window or area being recorded with a dashed line and dims the rest
// of the display around an area. Clicks go through it, and recordings leave it out. A whole
// display needs no guide.
func (c *Controller) showGuide() {
	c.closeWindow(&c.guide)
	if c.target == nil || c.target.Kind == settings.RecordDisplay {
		return
	}
	t := *c.target
	c.guide = c.openChrome(clickThrough, t.Display.Bounds, t.Display.Scale, func(ctx *ui.Context) { guideView(ctx, t) })
}

func guideView(ctx *ui.Context, t Target) {
	p := theme.Apply(ctx)
	ctx.Root().Background(ui.Transparent)
	area := t.Kind == settings.RecordArea
	if !area {
		ctx.After(200 * time.Millisecond)
	}
	f, d := t.frame(), t.Display.Bounds
	ui.Box(ctx).Fill().PassThrough().Draw(func(pt *ui.Painter, r ui.Rect) {
		s := pt.Scale()
		x, y := r.X+float32(f.X-d.X)/s, r.Y+float32(f.Y-d.Y)/s
		w, h := float32(f.W)/s, float32(f.H)/s
		if area {
			dimAround(pt, r, ui.Rect{X: x, Y: y, W: w, H: h}, shade)
		}
		pt.StrokeDashed(ui.Rect{X: x - 2, Y: y - 2, W: w + 4, H: h + 4}, p.Accent, 0, 2)
	})
}
