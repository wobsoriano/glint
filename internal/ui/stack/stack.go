// Package stack is the Quick Access stack: a column of capture cards at the bottom corner of
// the work area that floats above other apps without taking the keyboard. Hovering a card
// shows its actions where Settings > Overlay puts them; dragging it drops its file into another
// app, and letting it go past the screen edge dismisses it. The header tucks the stack into the
// edge as a peek tab.
package stack

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

// Host does what a card's actions ask for. Each call runs on a goroutine of its own and may
// block, as on a Save As dialog. Return context.Canceled when the user backs out: the card
// then says nothing.
type Host interface {
	Copy(store.Item) error
	// Save writes the deliverable to the export folder, or where the user chooses with choose.
	Save(item store.Item, choose bool) error
	Compress(store.Item) error
	CopyText(store.Item) error
	Edit(store.Item) error
	// Pin and QuickLook open the item's other surfaces; a pinned card leaves the stack.
	Pin(store.Item) error
	QuickLook(store.Item) error
	// Delete moves the capture to the trash; the card goes when it returns nil.
	Delete(store.Item) error
	// DragFile is the path a card hands out as it is dragged into another app.
	DragFile(store.Item) (string, error)
}

// effect is what the stack does around a host action.
type effect struct {
	run     func(Host, store.Item) error
	done    string // flashed on the card on success
	removes bool   // the card leaves the stack on success
}

var effects = map[settings.CardAction]effect{
	settings.CardCopy:      {run: Host.Copy, done: "Copied"},
	settings.CardSave:      {run: func(h Host, i store.Item) error { return h.Save(i, false) }, done: "Saved"},
	settings.CardSaveAs:    {run: func(h Host, i store.Item) error { return h.Save(i, true) }, done: "Saved"},
	settings.CardCompress:  {run: Host.Compress, done: "Compressed"},
	settings.CardCopyText:  {run: Host.CopyText, done: "Text copied"},
	settings.CardEdit:      {run: Host.Edit},
	settings.CardPin:       {run: Host.Pin, removes: true},
	settings.CardQuickLook: {run: Host.QuickLook},
	settings.CardDelete:    {run: Host.Delete, removes: true},
}

const (
	flashFor = 1300 * time.Millisecond
	// hoverGrace is the least time a card stays after the pointer leaves the stack.
	hoverGrace = 2 * time.Second
	tickEvery  = 250 * time.Millisecond
)

type card struct {
	id    int
	item  store.Item
	thumb *ui.Bitmap
	// deadline is when the card closes by itself; zero keeps it.
	deadline    time.Time
	status      string
	statusUntil time.Time
}

func (c *card) recording() bool { return c.item.Kind() == store.Recording }

// Stack is the window of cards. Its methods are safe from any goroutine; its state lives on
// the main thread.
type Stack struct {
	host Host

	prefs     settings.OverlayPrefs
	owner     *mygo.Window
	win       *mygo.Window
	shown     bool
	cards     []*card // oldest first
	nextID    int
	collapsed bool
	hovered   bool
	menu      int // the card whose More menu is open, 0 for none
	area      platform.Rect
	scale     float64
	ticking   bool
	now       func() time.Time
}

// New makes an empty stack; its window appears with the first Push.
func New(host Host, prefs settings.OverlayPrefs) *Stack {
	return &Stack{host: host, prefs: prefs, now: time.Now}
}

// Push adds a card for a screenshot or a recording at the top of the stack. Its thumbnail is
// the item's thumb.png, or for a recording without one its first frame.
func (s *Stack) Push(item store.Item) {
	thumb := ui.NewBitmap(loadThumb(item))
	mygo.RunOnMain(func() { s.add(item, thumb) })
}

// Refresh reloads the thumbnail of the item's card, as after an edit.
func (s *Stack) Refresh(item store.Item) {
	thumb := loadThumb(item)
	mygo.RunOnMain(func() {
		for _, c := range s.cards {
			if c.item.Dir == item.Dir {
				c.item = item
				c.thumb = ui.NewBitmap(thumb)
			}
		}
		s.invalidate()
	})
}

// SetPrefs applies Settings > Overlay: the side, the auto-close delay of new cards and the
// layout of the actions.
func (s *Stack) SetPrefs(p settings.OverlayPrefs) {
	mygo.RunOnMain(func() {
		s.prefs = p
		s.relayout()
		s.invalidate()
	})
}

// Clear closes every card.
func (s *Stack) Clear() { mygo.RunOnMain(func() { s.clear() }) }

// Len is the number of cards.
func (s *Stack) Len() (n int) {
	mygo.RunOnMain(func() { n = len(s.cards) })
	return n
}

// Window is the stack's window, nil before the first card.
func (s *Stack) Window() (w *mygo.Window) {
	mygo.RunOnMain(func() { w = s.win })
	return w
}

func (s *Stack) add(item store.Item, thumb *ui.Bitmap) {
	if s.win == nil {
		s.open()
	}
	if !s.shown {
		s.area, s.scale = workAreaAtCursor()
	}
	s.nextID++
	c := &card{id: s.nextID, item: item, thumb: thumb}
	if n := s.prefs.AutoCloseSeconds; n > 0 {
		c.deadline = s.now().Add(time.Duration(n) * time.Second)
	}
	s.cards = append(s.cards, c)
	if extra := len(s.cards) - capacity(float64(s.area.H)/s.scale); extra > 0 {
		s.cards = slices.Delete(s.cards, 0, extra)
	}
	s.collapsed = false
	s.relayout()
	if !s.shown {
		s.shown = true
		s.win.ShowInactive()
	}
	s.invalidate()
	s.startTicking()
}

func (s *Stack) open() {
	if s.owner == nil {
		s.owner = mygo.NewWindow(mygo.WindowOptions{Title: "Glint stack", Width: 1, Height: 1, Frameless: true, SkipTaskbar: true, Hidden: true})
	}
	w, h := stackSize(1, false)
	s.win = mygo.NewFlyout(mygo.FlyoutOptions{Parent: s.owner, Width: int(w), Height: int(h), Hidden: true, Content: ui.View(s.view)})
	s.win.OnClosed(func() { s.win, s.shown = nil, false })
}

func (s *Stack) relayout() {
	if s.win == nil || len(s.cards) == 0 {
		return
	}
	place(s.win, stackRect(s.area, s.scale, s.prefs.Side, len(s.cards), s.collapsed))
}

func (s *Stack) invalidate() {
	if s.win != nil {
		s.win.Invalidate()
	}
}

func (s *Stack) remove(id int) {
	s.cards = slices.DeleteFunc(s.cards, func(c *card) bool { return c.id == id })
	if s.menu == id {
		s.menu = 0
	}
	if len(s.cards) == 0 {
		s.clear()
		return
	}
	s.relayout()
	s.invalidate()
}

func (s *Stack) clear() {
	s.cards, s.menu, s.collapsed = nil, 0, false
	if s.win != nil && s.shown {
		s.win.Hide()
		s.shown = false
	}
}

func (s *Stack) card(id int) *card {
	for _, c := range s.cards {
		if c.id == id {
			return c
		}
	}
	return nil
}

func (s *Stack) flash(id int, msg string) {
	if c := s.card(id); c != nil {
		c.status, c.statusUntil = msg, s.now().Add(flashFor)
		s.invalidate()
	}
}

// run carries out an action of card id. Main thread only.
func (s *Stack) run(id int, a settings.CardAction) {
	c := s.card(id)
	if c == nil {
		return
	}
	if a != settings.CardMore {
		s.menu = 0
	}
	switch a {
	case settings.CardDismiss:
		s.remove(id)
		return
	case settings.CardMore:
		if s.menu == id {
			s.menu = 0
		} else {
			s.menu = id
		}
		s.invalidate()
		return
	}
	e, ok := effects[a]
	if !ok {
		return
	}
	item := c.item
	go func() {
		err := e.run(s.host, item)
		mygo.RunOnMain(func() {
			switch {
			case errors.Is(err, context.Canceled):
			case err != nil:
				s.flash(id, a.Label()+" failed: "+err.Error())
			case e.removes:
				s.remove(id)
			case e.done != "":
				s.flash(id, e.done)
			}
		})
	}()
	s.invalidate()
}

// dragged ends a drag of card id: one dropped in an app may close the card, and one let go
// over nothing past the stack's edge of the screen dismisses it.
func (s *Stack) dragged(id int, r transfer.Result) {
	switch {
	case r.Err != nil:
		s.flash(id, "Drag failed: "+r.Err.Error())
	case !r.Canceled:
		if s.prefs.DismissAfterDrag {
			s.remove(id)
		}
	case !r.Escaped:
		if x, _ := platform.CursorPos(); pastEdge(x, s.area, s.prefs.Side) {
			s.remove(id)
		}
	}
}

func (s *Stack) setHovered(h bool) {
	if h == s.hovered {
		return
	}
	s.hovered = h
	if h {
		return
	}
	floor := s.now().Add(hoverGrace)
	for _, c := range s.cards {
		if !c.deadline.IsZero() && c.deadline.Before(floor) {
			c.deadline = floor
		}
	}
}

func (s *Stack) startTicking() {
	if s.ticking {
		return
	}
	s.ticking = true
	go func() {
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		for range t.C {
			done := false
			mygo.RunOnMain(func() {
				s.tick()
				done = len(s.cards) == 0
				s.ticking = !done
			})
			if done {
				return
			}
		}
	}()
}

// tick clears flashed statuses and closes the cards whose time is up while the pointer is
// away and the stack is out. Main thread only.
func (s *Stack) tick() {
	now := s.now()
	changed := false
	for _, c := range s.cards {
		if c.status != "" && !now.Before(c.statusUntil) {
			c.status = ""
			changed = true
		}
	}
	if !s.hovered && !s.collapsed {
		n := len(s.cards)
		s.cards = slices.DeleteFunc(s.cards, func(c *card) bool { return !c.deadline.IsZero() && !now.Before(c.deadline) })
		if len(s.cards) != n {
			if len(s.cards) == 0 {
				s.clear()
				return
			}
			s.relayout()
			changed = true
		}
	}
	if changed {
		s.invalidate()
	}
}

func (s *Stack) dragData(id int) transfer.Data {
	c := s.card(id)
	if c == nil {
		return transfer.Data{}
	}
	path, err := s.host.DragFile(c.item)
	if err == nil {
		var d transfer.Data
		if d, err = transfer.FileData(path); err == nil {
			return d
		}
	}
	s.flash(id, "Drag failed: "+err.Error())
	return transfer.Data{}
}

// loadThumb reads the item's thumbnail, falling back to a recording's first frame and then to
// a dark 16:9 tile.
func loadThumb(item store.Item) image.Image {
	if f, err := os.Open(item.Thumb()); err == nil {
		img, err := png.Decode(f)
		f.Close()
		if err == nil {
			return img
		}
	}
	if item.Kind() == store.Recording {
		if r, err := media.OpenVideoReader(item.Deliverable()); err == nil {
			f, err := r.NextFrame()
			r.Close()
			if err == nil {
				return store.Thumbnail(f.Image)
			}
		}
	}
	tile := image.NewRGBA(image.Rect(0, 0, 16, 9))
	draw.Draw(tile, tile.Bounds(), image.NewUniform(color.RGBA{17, 17, 19, 255}), image.Point{}, draw.Src)
	return tile
}
