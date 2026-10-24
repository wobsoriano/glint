package studio

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/wobsoriano/glint/internal/media/input"
)

// TestRustParity compares auto zoom and every motion track against dumps written by the Rust
// build for real recordings. Make the dumps from copies of Library sessions, never the
// Library itself:
//
//	(in a checkout of f1d58d2, the last Rust commit) cargo run -p glint-studio --example go_fixtures -- <out> <copy>/*.glintrec
//	GLINT_STUDIO_PARITY=<out> go test ./internal/studio -run TestRustParity -v
func TestRustParity(t *testing.T) {
	out := os.Getenv("GLINT_STUDIO_PARITY")
	if out == "" {
		t.Skip("GLINT_STUDIO_PARITY is not set")
	}
	files, err := filepath.Glob(filepath.Join(out, "parity", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no parity dumps in %s: %v", out, err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) { checkParity(t, f) })
	}
}

type paritySample struct {
	T        float64      `json:"t"`
	Viewport [3]float64   `json:"viewport"`
	Pointer  *[4]float64  `json:"pointer"`
	Clicks   [][7]float64 `json:"clicks"`
	Key      *[4]any      `json:"key"`
}

func checkParity(t *testing.T, file string) {
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var dump struct {
		Dir        string  `json:"dir"`
		DurationMs float64 `json:"duration_ms"`
		AutoZooms  string  `json:"auto_zooms"`
		Projects   []struct {
			Name    string         `json:"name"`
			Project string         `json:"project"`
			Samples []paritySample `json:"samples"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &dump); err != nil {
		t.Fatal(err)
	}
	in, err := input.Load(dump.Dir)
	if err != nil {
		t.Fatal(err)
	}
	zooms := AutoZooms(&in, dump.DurationMs)
	got, _ := json.Marshal(zooms)
	if zooms == nil {
		got = []byte("[]")
	}
	sameJSON(t, got, []byte(dump.AutoZooms))
	t.Logf("duration %v ms, %d clicks, auto zooms %s", dump.DurationMs, len(in.Clicks), got)

	s, err := OpenSession(dump.Dir, in.Size[0], in.Size[1], dump.DurationMs)
	if err != nil {
		t.Fatal(err)
	}
	opened, _ := json.Marshal(s.Project)
	sameJSON(t, opened, []byte(dump.Projects[0].Project))

	for _, pd := range dump.Projects {
		var p Project
		if err := json.Unmarshal([]byte(pd.Project), &p); err != nil {
			t.Fatal(err)
		}
		m := BuildMotion(&p, &in, nil)
		var maxDiff float64
		var inexact int
		cmp := func(what string, ts float64, a, b float64) {
			if a != b {
				inexact++
				maxDiff = max(maxDiff, math.Abs(a-b))
				if math.Abs(a-b) > 1e-9 {
					t.Errorf("%s %s at %v ms: go %v rust %v", pd.Name, what, ts, a, b)
				}
			}
		}
		for _, s := range pd.Samples {
			v := m.Viewport(s.T)
			for i, g := range []float64{v.Scale, v.X, v.Y} {
				cmp("viewport", s.T, g, s.Viewport[i])
			}
			f, ok := m.Pointer.At(s.T)
			if ok != (s.Pointer != nil) {
				t.Fatalf("%s pointer presence at %v: go %v", pd.Name, s.T, ok)
			}
			if ok {
				for i, g := range []float64{f.X, f.Y, f.PressScale, float64(f.Cursor)} {
					cmp("pointer", s.T, g, s.Pointer[i])
				}
			}
			clicks := m.Clicks.At(s.T, 10)
			if len(clicks) != len(s.Clicks) {
				t.Fatalf("%s clicks at %v: go %d rust %d", pd.Name, s.T, len(clicks), len(s.Clicks))
			}
			for i, c := range clicks {
				for j, g := range []float64{c.X, c.Y, c.ImpactRadius, c.ImpactAlpha, c.RingRadius, c.RingAlpha, c.RingWidth} {
					cmp("click", s.T, g, s.Clicks[i][j])
				}
			}
			k, ok := m.Keys.At(s.T)
			if ok != (s.Key != nil) {
				t.Fatalf("%s key presence at %v: go %v", pd.Name, s.T, ok)
			}
			if ok {
				if k.Modifiers != s.Key[0] || k.Key != s.Key[1] {
					t.Fatalf("%s key label at %v: go %q %q rust %v", pd.Name, s.T, k.Modifiers, k.Key, s.Key)
				}
				cmp("key opacity", s.T, k.Opacity, s.Key[2].(float64))
				cmp("key scale", s.T, k.Scale, s.Key[3].(float64))
			}
		}
		t.Logf("%s: %d samples, %d zooms, %d values not bit-identical, max abs diff %g",
			pd.Name, len(pd.Samples), len(p.Zooms), inexact, maxDiff)
	}
}
