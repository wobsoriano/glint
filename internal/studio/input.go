package studio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// This file is a stand-in for internal/media/input, which owns input.json. It decodes only
// what Studio reads. Swap these types for the shared ones when that package lands.

const InputFile = "input.json"

// InputTimeline is the recorded pointer, clicks and keys. Times are media milliseconds with
// pauses removed; positions are screen-master pixels and may fall outside the frame.
type InputTimeline struct {
	Version uint32          `json:"version"`
	Size    [2]uint32       `json:"size"`
	Pointer []PointerSample `json:"pointer"`
	Clicks  []Click         `json:"clicks"`
	Keys    []KeyPress      `json:"keys"`
}

// PointerSample is one `[t_ms, x, y, cursor_id]` row.
type PointerSample struct {
	T      uint64
	X, Y   float32
	Cursor uint32
}

func (s *PointerSample) UnmarshalJSON(data []byte) error {
	var row []json.RawMessage
	if err := json.Unmarshal(data, &row); err != nil {
		return err
	}
	if len(row) != 4 {
		return fmt.Errorf("pointer sample has %d fields, want 4", len(row))
	}
	for i, dst := range []any{&s.T, &s.X, &s.Y, &s.Cursor} {
		if err := json.Unmarshal(row[i], dst); err != nil {
			return err
		}
	}
	return nil
}

type Click struct {
	T      uint64  `json:"t"`
	Up     uint64  `json:"up"`
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Button string  `json:"button"`
}

type KeyPress struct {
	T     uint64 `json:"t"`
	Label string `json:"label"`
}

func LoadInput(sessionDir string) (InputTimeline, error) {
	data, err := os.ReadFile(filepath.Join(sessionDir, InputFile))
	if err != nil {
		return InputTimeline{}, err
	}
	var in InputTimeline
	if err := json.Unmarshal(data, &in); err != nil {
		return InputTimeline{}, fmt.Errorf("parsing %s: %w", InputFile, err)
	}
	return in, nil
}
