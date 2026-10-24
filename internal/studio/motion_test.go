package studio

import (
	"math"
	"testing"
)

func click(t uint64, x, y float32) Click {
	return Click{T: t, Up: t + 100, X: x, Y: y, Button: "left"}
}

func TestAutoZoomClustersClicksWithLeadInAndHold(t *testing.T) {
	in := InputTimeline{Version: 1, Size: [2]uint32{1920, 1080}, Clicks: []Click{
		click(2_000, 100, 100), click(3_000, 200, 100), click(9_000, 900, 500),
		click(10_000, 5_000, 500), click(19_500, 500, 500),
	}}
	zooms := AutoZooms(&in, 20_000)
	var spans [][2]float64
	for _, z := range zooms {
		spans = append(spans, [2]float64{z.StartMs, z.EndMs})
		if !z.Auto || z.Scale != 1.5 || z.Focus != (ZoomFocus{}) {
			t.Fatalf("%+v", z)
		}
	}
	eq(t, spans, [][2]float64{{1_700, 5_500}, {8_700, 11_500}})
}

func TestRegeneratingAutoZoomKeepsManualZooms(t *testing.T) {
	in := InputTimeline{Version: 1, Size: [2]uint32{100, 100}, Clicks: []Click{click(1_000, 50, 50)}}
	p := NewProject(10_000)
	p.AddZoom(Span{6_000, 7_000})
	p.ApplyAutoZoom(&in, 10_000)
	p.ApplyAutoZoom(&in, 10_000)
	type view struct {
		S, E float64
		Auto bool
	}
	var got []view
	for _, z := range p.Zooms {
		got = append(got, view{z.StartMs, z.EndMs, z.Auto})
	}
	eq(t, got, []view{{700, 3_500, true}, {6_000, 7_000, false}})
}

func at(t *testing.T, track []Viewport, ms float64) Viewport {
	t.Helper()
	v, ok := sample(track, ms)
	if !ok {
		t.Fatal("empty track")
	}
	return v
}

func TestCameraEasesInHoldsAndEasesOut(t *testing.T) {
	p := NewProject(8_000)
	p.Zooms = append(p.Zooms, Zoom{StartMs: 1_000, EndMs: 4_000, Scale: 2, Focus: FixedFocus(0.25, 0.25), Enabled: true})
	in := InputTimeline{Version: 1, Size: [2]uint32{1000, 1000}}
	track := cameraTrack(&p, &in)
	eq(t, at(t, track, 500), IdentityViewport)
	if e := at(t, track, 1_150); !(e.Scale > 1 && e.Scale < 2) {
		t.Fatalf("easing %+v", e)
	}
	if h := at(t, track, 3_500); math.Abs(h.Scale-2) >= 0.01 || math.Abs(h.X-0.25) >= 0.01 {
		t.Fatalf("held %+v", h)
	}
	if o := at(t, track, 7_500); math.Abs(o.Scale-1) >= 0.01 || math.Abs(o.X-0.5) >= 0.01 {
		t.Fatalf("out %+v", o)
	}
}

func TestCameraFollowsPointerInsideAZoom(t *testing.T) {
	p := NewProject(6_000)
	p.Zooms = append(p.Zooms, Zoom{StartMs: 0, EndMs: 6_000, Scale: 2, Enabled: true})
	in := InputTimeline{Version: 1, Size: [2]uint32{1000, 1000}, Pointer: []PointerSample{{0, 200, 500, 0}, {2_000, 800, 500, 0}}}
	track := cameraTrack(&p, &in)
	if x := at(t, track, 1_900).X; x >= 0.35 {
		t.Fatalf("framing the left: %v", x)
	}
	if x := at(t, track, 5_000).X; x <= 0.65 {
		t.Fatalf("followed to the right: %v", x)
	}
}

func TestCursorGlidesTowardSamplesAndLandsOnClicks(t *testing.T) {
	p := NewProject(3_000)
	in := InputTimeline{
		Version: 1,
		Size:    [2]uint32{1000, 1000},
		Pointer: []PointerSample{{0, 100, 100, 0}, {500, 900, 100, 0}},
		Clicks:  []Click{{T: 2_000, Up: 2_100, X: 500, Y: 800, Button: "left"}},
	}
	track := BuildPointerTrack(&p, &in)
	at := func(ms float64) PointerFrame {
		f, ok := track.At(ms)
		if !ok {
			t.Fatal("empty track")
		}
		return f
	}
	eq(t, [2]float64{at(0).X, at(0).Y}, [2]float64{100, 100})
	if mid := at(550).X; !(mid > 100 && mid < 900) {
		t.Fatalf("spring is still travelling at 550 ms: %v", mid)
	}
	if math.Abs(at(1_400).X-900) >= 2 {
		t.Fatal("settled on the sample by 1.4 s")
	}
	if l := at(2_000); math.Abs(l.X-500) >= 10 || math.Abs(l.Y-800) >= 10 {
		t.Fatalf("landed %+v", l)
	}
	if at(2_100).PressScale >= 0.9 {
		t.Fatal("shrinks while pressed")
	}
	if at(2_900).PressScale <= 0.98 {
		t.Fatal("recovers after release")
	}
}
