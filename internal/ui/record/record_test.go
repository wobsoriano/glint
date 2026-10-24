package record

import (
	"math"
	"reflect"
	"runtime"
	"testing"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
)

func TestClockShowsMinutesThenHours(t *testing.T) {
	for ms, want := range map[uint64]string{0: "00:00", 61_999: "01:01", 3_725_000: "1:02:05"} {
		if got := clock(ms); got != want {
			t.Errorf("clock(%d) = %q, want %q", ms, got, want)
		}
	}
}

func TestMeterMapsRMSToADecibelScale(t *testing.T) {
	if meterFraction(0) != 0 || meterFraction(1) != 1 {
		t.Fatalf("silence and full scale: %v, %v", meterFraction(0), meterFraction(1))
	}
	if got := meterFraction(0.01); math.Abs(float64(got)-1.0/3) > 1e-4 {
		t.Errorf("-40 dB is a third of the way, got %v", got)
	}
}

func TestAChosenDeviceThatIsNotConnectedIsMissing(t *testing.T) {
	webcam := []namedDevice{{"usb#webcam", "Webcam"}}
	id := func(s string) *string { return &s }
	cases := []struct {
		selected *string
		loaded   bool
		devices  []namedDevice
		want     device
		name     string
	}{
		{nil, true, webcam, deviceOff, ""},
		{id("usb#gone"), false, webcam, deviceLoading, ""},
		{id("usb#webcam"), true, webcam, devicePresent, "Webcam"},
		{id("usb#gone"), true, webcam, deviceMissing, ""},
		{id("usb#gone"), true, nil, deviceMissing, ""},
	}
	for _, c := range cases {
		if got, name := deviceChoice(c.selected, c.loaded, c.devices); got != c.want || name != c.name {
			t.Errorf("deviceChoice(%v, %v) = %v %q, want %v %q", c.selected, c.loaded, got, name, c.want, c.name)
		}
	}
}

func TestRecordedLabelsFollowTheInputTrackOrder(t *testing.T) {
	if runtime.GOOS == "darwin" {
		for accel, want := range map[string]string{"Ctrl+Shift+7": "âŒƒâ‡§7", "Super+Shift+5": "â‡§âŒ˜5", "F5": "F5"} {
			if got := recordedLabel(accel); got != want {
				t.Errorf("recordedLabel(%q) = %q, want %q", accel, got, want)
			}
		}
		return
	}
	for accel, want := range map[string]string{"Ctrl+Shift+7": "Ctrl+Shift+7", "Ctrl+Shift+Super+L": "Win+Ctrl+Shift+L", "Printscreen": "PrintScreen"} {
		if got := recordedLabel(accel); got != want {
			t.Errorf("recordedLabel(%q) = %q, want %q", accel, got, want)
		}
	}
}

func TestIgnoredKeysAreEveryBoundHotkey(t *testing.T) {
	h := settings.Default().Hotkeys
	h.Library = ""
	got := ignoredKeys(h)
	if len(got) != len(settings.HotkeyActions)-1 {
		t.Fatalf("one label per bound hotkey, got %v", got)
	}
	want := recordedLabel(h.Recording)
	found := false
	for _, k := range got {
		found = found || k == want
	}
	if !found {
		t.Errorf("the Record hotkey %q is not ignored: %v", want, got)
	}
}

func TestWarningSummaryKeepsEveryWarningAndDropsDeviceDetail(t *testing.T) {
	got := warningSummary([]string{
		"microphone not recorded: microphone {0.0.1.00000000}.{deadbeef}: Element not found.",
		`camera not recorded: no camera with id \\?\usb#gone`,
		"could not disable the capture border",
	})
	if want := "microphone not recorded · camera not recorded · could not disable the capture border"; got != want {
		t.Errorf("got %q", got)
	}
}

func TestWrapsWordsGreedilyAndKeepsLongWordsWhole(t *testing.T) {
	if got := wrap([]float32{30, 30, 30, 30}, 10, 75); !reflect.DeepEqual(got, []span{{0, 2}, {2, 4}}) {
		t.Errorf("got %v", got)
	}
	if got := wrap([]float32{100, 20}, 10, 75); !reflect.DeepEqual(got, []span{{0, 1}, {1, 2}}) {
		t.Errorf("a word wider than a line gets its own: %v", got)
	}
	if got := wrap(nil, 10, 75); len(got) != 0 {
		t.Errorf("no words, no lines: %v", got)
	}
}

func TestTheWheelMovesThePrompterByWholeLinesWithinTheScript(t *testing.T) {
	pr := newPrompter("", 2, nil)
	pr.lines = make([]span, 5)
	pr.scroll(prompterLineH / 2)
	if pr.first() != 0 {
		t.Fatalf("half a line stays put, first line %d", pr.first())
	}
	pr.scroll(prompterLineH / 2)
	if pr.first() != 1 {
		t.Fatalf("a whole line moves one, first line %d", pr.first())
	}
	pr.scroll(prompterLineH * 10)
	if pr.first() != 3 {
		t.Fatalf("never past the last full page, first line %d", pr.first())
	}
	pr.scroll(-prompterLineH * 10)
	if pr.first() != 0 {
		t.Fatalf("never above the first line, first line %d", pr.first())
	}
}

var (
	left    = platform.Display{ID: 2, Name: "Display 1", Bounds: platform.Rect{X: -1920, Y: 0, W: 1920, H: 1080}, Scale: 1}
	primary = platform.Display{ID: 1, Name: "Display 2", Bounds: platform.Rect{X: 0, Y: 0, W: 2560, H: 1440}, Scale: 1.5, Primary: true}
	desktop = []platform.Display{primary, left}
)

func TestAreasAreClippedToTheDisplayAndMadeEven(t *testing.T) {
	d := left.Bounds
	if got, ok := clipArea(platform.Rect{X: -2000, Y: 100, W: 501, H: 301}, d); !ok || got != (platform.Rect{X: -1920, Y: 100, W: 420, H: 300}) {
		t.Errorf("got %v %v", got, ok)
	}
	if _, ok := clipArea(platform.Rect{X: -100, Y: 1070, W: 400, H: 300}, d); ok {
		t.Error("10 px tall after clipping is too small")
	}
	if _, ok := clipArea(platform.Rect{X: 0, Y: 0, W: 50, H: 50}, d); ok {
		t.Error("outside the display")
	}
}

func TestASelectionIsAWindowADisplayOrAnArea(t *testing.T) {
	win := platform.Window{ID: 77, Title: "Notes", Bounds: platform.Rect{X: -1000, Y: 200, W: 600, H: 400}}
	got, ok := fromSelection(desktop, Selection{Rect: win.Bounds, Window: &win})
	if !ok || got.Kind != settings.RecordWindow || got.Display.ID != left.ID || got.Source() != (media.WindowSource{Window: 77}) || got.Label() != "Notes" {
		t.Errorf("window: %+v %v", got, ok)
	}
	got, ok = fromSelection(desktop, Selection{Rect: primary.Bounds})
	if !ok || got.Kind != settings.RecordDisplay || got.Source() != (media.DisplaySource{Display: 1}) || got.Label() != "Display 2" {
		t.Errorf("display: %+v %v", got, ok)
	}
	got, ok = fromSelection(desktop, Selection{Rect: platform.Rect{X: 2300, Y: 100, W: 401, H: 301}})
	want := platform.Rect{X: 2300, Y: 100, W: 260, H: 300}
	if !ok || got.Kind != settings.RecordArea || got.Area != want || got.Source() != (media.AreaSource{Display: 1, Rect: want}) {
		t.Errorf("area: %+v %v", got, ok)
	}
	if got.Label() != "260 × 300 area of Display 2" {
		t.Errorf("area label %q", got.Label())
	}
	if _, ok := fromSelection(desktop, Selection{Rect: platform.Rect{X: 100, Y: 100, W: 8, H: 8}}); ok {
		t.Error("an 8 px area is too small to record")
	}
}

func TestAreaPrefsRoundTrip(t *testing.T) {
	r := platform.Rect{X: -300, Y: 20, W: 640, H: 360}
	if got := prefArea(*areaPref(r)); got != r {
		t.Errorf("got %v", got)
	}
}
