// Package input records the pointer, clicks, shortcuts and cursor shapes beside the screen
// master as input.json. Times are media milliseconds with pauses removed; positions are
// screen-master pixels.
package input

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// File is the timeline's name inside a session directory.
const File = "input.json"

type Timeline struct {
	Version uint32    `json:"version"`
	Size    [2]uint32 `json:"size"`
	// Pointer is sorted by time.
	Pointer []PointerSample `json:"pointer"`
	Clicks  []Click         `json:"clicks"`
	Keys    []KeyPress      `json:"keys"`
	Cursors []CursorSprite  `json:"cursors"`
}

// PointerSample is written as [t_ms, x, y, cursor_id].
type PointerSample struct {
	T      uint64
	X, Y   float32
	Cursor uint32
}

type MouseButton uint8

const (
	ButtonLeft MouseButton = iota
	ButtonRight
	ButtonMiddle
)

var buttonNames = [...]string{"left", "right", "middle"}

type Click struct {
	T      uint64      `json:"t"`
	Up     uint64      `json:"up"`
	X      float32     `json:"x"`
	Y      float32     `json:"y"`
	Button MouseButton `json:"button"`
}

type KeyPress struct {
	T     uint64 `json:"t"`
	Label string `json:"label"`
}

type CursorSprite struct {
	ID uint32 `json:"id"`
	// File is relative to the session directory, such as cursors/0.png.
	File    string    `json:"file"`
	Hotspot [2]uint32 `json:"hotspot"`
	Scale   float32   `json:"scale"`
}

func (b MouseButton) MarshalText() ([]byte, error) {
	if int(b) >= len(buttonNames) {
		return nil, fmt.Errorf("mouse button %d", b)
	}
	return []byte(buttonNames[b]), nil
}

func (b *MouseButton) UnmarshalText(text []byte) error {
	for i, n := range buttonNames {
		if n == string(text) {
			*b = MouseButton(i)
			return nil
		}
	}
	return fmt.Errorf("unknown mouse button %q", text)
}

func (p PointerSample) MarshalJSON() ([]byte, error) {
	return json.Marshal([4]any{p.T, p.X, p.Y, p.Cursor})
}

func (p *PointerSample) UnmarshalJSON(b []byte) error {
	var row []json.RawMessage
	if err := json.Unmarshal(b, &row); err != nil {
		return err
	}
	if len(row) != 4 {
		return fmt.Errorf("pointer sample has %d fields, want 4", len(row))
	}
	for i, dst := range []any{&p.T, &p.X, &p.Y, &p.Cursor} {
		if err := json.Unmarshal(row[i], dst); err != nil {
			return fmt.Errorf("pointer sample field %d: %w", i, err)
		}
	}
	return nil
}

// MarshalJSON writes empty lists as [], since the Rust reader rejects null.
func (t Timeline) MarshalJSON() ([]byte, error) {
	type plain Timeline
	p := plain(t)
	p.Pointer = orEmpty(p.Pointer)
	p.Clicks = orEmpty(p.Clicks)
	p.Keys = orEmpty(p.Keys)
	p.Cursors = orEmpty(p.Cursors)
	return json.Marshal(p)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Load reads input.json from a session directory.
func Load(sessionDir string) (Timeline, error) {
	var t Timeline
	b, err := os.ReadFile(filepath.Join(sessionDir, File))
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return t, fmt.Errorf("%s: %w", File, err)
	}
	return t, nil
}

// Save writes input.json into a session directory.
func (t Timeline) Save(sessionDir string) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sessionDir, File), b, 0o644)
}
