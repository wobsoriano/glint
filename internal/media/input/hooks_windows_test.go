package input

import (
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func newTestHookState(t *testing.T, m PointerMap) *hookState {
	return newHookState(Options{Map: m, Size: [2]uint32{1920, 1080}, Scale: 1.5, Clock: runningClock()}, t.TempDir())
}

func labels(s *hookState) []string {
	out := []string{}
	for _, k := range s.rec.tl.Keys {
		out = append(out, k.Label)
	}
	return out
}

const (
	scQ             = 0x10
	scS             = 0x1F
	scA             = 0x1E
	scLControl      = 0x1D
	scReturn        = 0x1C
	scAlt           = 0x38
	scAltGrLControl = 0x21D
	scRShift        = 0x36
	vkS, vkA, vkQ   = 'S', 'A', 'Q'
)

func TestHookPointerMovesAreMappedAndRateLimited(t *testing.T) {
	s := newTestHookState(t, FixedOrigin(100, 50))
	s.onMouse(wmMouseMove, 110, 70)
	s.onMouse(wmMouseMove, 111, 71)
	if p := s.rec.tl.Pointer; len(p) != 1 || p[0].X != 10 || p[0].Y != 20 || p[0].Cursor != 0 {
		t.Fatalf("a second move within 8 ms is dropped: %v", p)
	}
}

func TestHookButtonsBecomeClicks(t *testing.T) {
	s := newTestHookState(t, FixedOrigin(0, 0))
	for _, m := range []uint32{wmLButtonDown, wmLButtonUp, wmRButtonDown, wmRButtonUp, wmMButtonDown, wmMButtonUp} {
		s.onMouse(m, 5, 6)
	}
	var got []MouseButton
	for _, c := range s.rec.tl.Clicks {
		got = append(got, c.Button)
	}
	if !reflect.DeepEqual(got, []MouseButton{ButtonLeft, ButtonRight, ButtonMiddle}) {
		t.Fatal(got)
	}
}

func TestWindowPointerMapFollowsTheWindowAndKeepsTheLastOrigin(t *testing.T) {
	name, _ := windows.UTF16PtrFromString("Shell_TrayWnd")
	hwnd, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(name)), 0)
	if hwnd == 0 {
		t.Skip("no taskbar window")
	}
	prev, _, _ := procSetThreadDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
	defer procSetThreadDpiAwarenessContext.Call(prev)
	origin, ok := windowOrigin(int64(hwnd), BoundsWindowRect)
	if !ok {
		t.Fatal("GetWindowRect failed on the taskbar")
	}
	m := WindowOrigin(int64(hwnd), BoundsWindowRect)
	if p := m.toCapture(origin.X+7, origin.Y+9); p != (Point{7, 9}) {
		t.Fatalf("taskbar at %v mapped to %v", origin, p)
	}
	m.window = 1
	if p := m.toCapture(origin.X, origin.Y); p != (Point{0, 0}) || m.origin != origin {
		t.Fatalf("a window that is gone keeps its last origin: %v %v", p, m.origin)
	}
	if _, ok := windowOrigin(1, BoundsDWM); ok {
		t.Fatal("no window has handle 1")
	}
}

func altgrChord(s *hookState, vk, scan uint32) {
	s.onKey(wmSysKeyDown, vkLControl, scAltGrLControl)
	s.onKey(wmSysKeyDown, vkRMenu, scAlt)
	s.onKey(wmSysKeyDown, vk, scan)
	s.onKey(wmSysKeyUp, vk, scan)
	s.onKey(wmKeyUp, vkLControl, scAltGrLControl)
	s.onKey(wmKeyUp, vkRMenu, scAlt)
}

func TestKeysRecordShortcutsOncePerPress(t *testing.T) {
	s := newTestHookState(t, FixedOrigin(0, 0))
	s.onKey(wmKeyDown, vkLControl, scLControl)
	s.onKey(wmKeyDown, vkS, scS)
	s.onKey(wmKeyDown, vkS, scS)
	s.onKey(wmKeyUp, vkS, scS)
	s.onKey(wmKeyUp, vkLControl, scLControl)
	s.onKey(wmKeyDown, vkA, scA)
	s.onKey(wmKeyDown, vkReturn, scReturn)
	if got := labels(s); !reflect.DeepEqual(got, []string{"Ctrl+S", "Enter"}) {
		t.Fatal(got)
	}
}

func TestAltGrTypingIsNotAShortcut(t *testing.T) {
	s := newTestHookState(t, FixedOrigin(0, 0))
	altgrChord(s, vkQ, scQ)
	if got := labels(s); len(got) != 0 {
		t.Fatalf("AltGr+Q types @ on a German layout: %v", got)
	}
	altgrChord(s, vkReturn, scReturn)
	if got := labels(s); !reflect.DeepEqual(got, []string{"Enter"}) {
		t.Fatalf("AltGr+Enter is still the Enter key: %v", got)
	}
	s.onKey(wmKeyDown, vkQ, scQ)
	if got := labels(s); !reflect.DeepEqual(got, []string{"Enter"}) {
		t.Fatalf("AltGr is fully released afterwards: %v", got)
	}
}

func TestLeftCtrlLeftAltIsStillAShortcut(t *testing.T) {
	s := newTestHookState(t, FixedOrigin(0, 0))
	s.onKey(wmKeyDown, vkLControl, scLControl)
	s.onKey(wmSysKeyDown, vkLMenu, scAlt)
	s.onKey(wmSysKeyDown, vkQ, scQ)
	s.onKey(wmSysKeyUp, vkQ, scQ)
	s.onKey(wmKeyUp, vkLMenu, scAlt)
	s.onKey(wmKeyUp, vkLControl, scLControl)
	if got := labels(s); !reflect.DeepEqual(got, []string{"Ctrl+Alt+Q"}) {
		t.Fatal(got)
	}
}

func TestIgnoredHotkeysNeverReachTheTimeline(t *testing.T) {
	s := newHookState(Options{Map: FixedOrigin(0, 0), Size: [2]uint32{10, 10}, Clock: runningClock(), IgnoredKeys: []string{"Ctrl+Shift+7"}}, t.TempDir())
	s.onKey(wmKeyDown, vkLControl, scLControl)
	s.onKey(wmKeyDown, vkLShift, 0x2A)
	s.onKey(wmKeyDown, '7', 0x08)
	s.onKey(wmKeyUp, '7', 0x08)
	s.onKey(wmKeyDown, '8', 0x09)
	if got := labels(s); !reflect.DeepEqual(got, []string{"Ctrl+Shift+8"}) {
		t.Fatal(got)
	}
}

func TestHookRecordsNothingWhilePaused(t *testing.T) {
	s := newTestHookState(t, FixedOrigin(0, 0))
	s.rec.clock.Pause()
	s.onMouse(wmMouseMove, 1, 1)
	s.onMouse(wmLButtonDown, 1, 1)
	s.onKey(wmKeyDown, vkReturn, scReturn)
	if !reflect.DeepEqual(s.rec.tl, Timeline{Version: 1, Size: [2]uint32{1920, 1080}}) {
		t.Fatalf("%+v", s.rec.tl)
	}
}

func TestPausedSpansAreLeftOutOfInputTime(t *testing.T) {
	s := newTestHookState(t, FixedOrigin(0, 0))
	clock := s.rec.clock
	time.Sleep(40 * time.Millisecond)
	s.onKey(wmKeyDown, vkReturn, scReturn)
	clock.Pause()
	time.Sleep(200 * time.Millisecond)
	clock.Resume()
	s.onKey(wmKeyDown, vkEscape, 0x01)
	k := s.rec.tl.Keys
	if len(k) != 2 || k[1].T-k[0].T > 30 {
		t.Fatalf("the 200 ms pause leaked into input time: %+v", k)
	}
}

var (
	none      = mods{}
	ctrl      = mods{ctrl: true}
	ctrlShift = mods{ctrl: true, shift: true}
	shift     = mods{shift: true}
	altgr     = mods{altgr: true}
)

func TestModifierCombosAreLabelled(t *testing.T) {
	cases := []struct {
		vk   uint32
		m    mods
		want string
	}{
		{'S', ctrlShift, "Ctrl+Shift+S"},
		{'C', ctrl, "Ctrl+C"},
		{vkDelete, mods{ctrl: true, alt: true}, "Ctrl+Alt+Delete"},
		{'S', mods{win: true, shift: true}, "Win+Shift+S"},
		{vkF1 + 4, none, "F5"},
		{vkTab, shift, "Shift+Tab"},
		{vkReturn, none, "Enter"},
		{vkEscape, none, "Esc"},
		{vkBack, none, "Backspace"},
		{vkLeft, none, "Left"},
		{vkNumpad0 + 3, ctrl, "Ctrl+Num3"},
		{vkF24, none, "F24"},
	}
	for _, c := range cases {
		if got := shortcutLabel(c.vk, c.m); got != c.want {
			t.Errorf("vk 0x%X %+v: %q, want %q", c.vk, c.m, got, c.want)
		}
	}
}

func TestPlainTypingAndLoneModifiersAreNotRecorded(t *testing.T) {
	cases := []struct {
		vk uint32
		m  mods
	}{
		{'A', none}, {'A', shift}, {'1', none}, {vkSpace, none},
		{vkLControl, ctrl}, {vkLShift, shift}, {vkLWin, mods{win: true}},
	}
	for _, c := range cases {
		if got := shortcutLabel(c.vk, c.m); got != "" {
			t.Errorf("vk 0x%X: %q", c.vk, got)
		}
	}
}

func TestOEMKeysAreNamedByTheLayout(t *testing.T) {
	const vkOEMComma = 0xBC
	if got := shortcutLabel(vkOEMComma, ctrl); got == "" || !strings.HasPrefix(got, "Ctrl+") {
		t.Fatalf("Ctrl+comma labelled %q", got)
	}
}

func TestModifierStateTracksDownAndUp(t *testing.T) {
	var m mods
	if !m.apply(vkLControl, scLControl, true) || !m.apply(vkRShift, scRShift, true) || m != ctrlShift {
		t.Fatalf("%+v", m)
	}
	if m.apply('S', scS, true) {
		t.Fatal("S is not a modifier")
	}
	if !m.apply(vkLControl, scLControl, false) || m != shift {
		t.Fatalf("%+v", m)
	}
}

func TestAltGrIsItsOwnModifierNotCtrlAlt(t *testing.T) {
	var m mods
	if !m.apply(vkLControl, scAltGrLControl, true) || !m.apply(vkRMenu, scAlt, true) || m != altgr {
		t.Fatalf("%+v", m)
	}
	if shortcutLabel('Q', m) != "" || shortcutLabel(vkReturn, m) != "Enter" {
		t.Fatal("AltGr+Q is typing, AltGr+Enter is Enter")
	}
	if !m.apply(vkLControl, scAltGrLControl, false) || !m.apply(vkRMenu, scAlt, false) || m != none {
		t.Fatalf("%+v", m)
	}
}

func TestRightAltWithoutTheSyntheticCtrlIsAlt(t *testing.T) {
	var m mods
	m.apply(vkLControl, scLControl, true)
	m.apply(vkRMenu, scAlt, true)
	if m != (mods{ctrl: true, alt: true}) || shortcutLabel('Q', m) != "Ctrl+Alt+Q" {
		t.Fatalf("%+v", m)
	}
}

func TestMergeRecoversAlphaAndColour(t *testing.T) {
	black := []byte{0, 0, 128, 0, 0, 0, 0, 0, 0, 0, 255, 0}
	white := []byte{127, 127, 255, 0, 255, 255, 255, 0, 0, 0, 255, 0}
	got := mergeOnBlackWhite(black, white)
	want := []byte{255, 0, 0, 128, 0, 0, 0, 0, 255, 0, 0, 255}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("half-transparent red, clear, opaque red: %v", got)
	}
}

func TestRendersTheArrowCursor(t *testing.T) {
	const idcArrow = 32512
	arrow, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("LoadCursorW").Call(0, idcArrow)
	if arrow == 0 {
		t.Fatal("LoadCursorW")
	}
	img, ok := renderCursor(arrow)
	if !ok {
		t.Fatal("renderCursor failed")
	}
	if img.width < 16 || img.height < 16 {
		t.Fatalf("%dx%d", img.width, img.height)
	}
	opaque, clear := 0, 0
	for i := 3; i < len(img.rgba); i += 4 {
		switch img.rgba[i] {
		case 255:
			opaque++
		case 0:
			clear++
		}
	}
	if opaque <= 50 || clear <= opaque {
		t.Fatalf("an arrow has a solid body in mostly empty space: %d opaque, %d clear", opaque, clear)
	}
	if img.hotspot != [2]uint32{0, 0} {
		t.Fatalf("the arrow's tip is its hotspot: %v", img.hotspot)
	}
	again, _ := renderCursor(arrow)
	if again.hash() != img.hash() {
		t.Fatal("rendering is deterministic")
	}
}

// The hook procedures are called the way Windows calls them, through their C entry points,
// with hand-built hook structs; no input reaches the desktop.
func TestHookProceduresDecodeWhatWindowsPasses(t *testing.T) {
	s := newTestHookState(t, FixedOrigin(100, 50))
	active = s
	defer func() { active = nil }()
	mouse := msllHookStruct{pt: point{130, 90}}
	syscall.SyscallN(mouseProc, hcAction, wmLButtonDown, uintptr(unsafe.Pointer(&mouse)))
	syscall.SyscallN(mouseProc, hcAction, wmLButtonUp, uintptr(unsafe.Pointer(&mouse)))
	for _, k := range []struct{ msg, vk, scan uint32 }{
		{wmKeyDown, vkLControl, scLControl}, {wmKeyDown, vkS, scS}, {wmKeyUp, vkS, scS}, {wmKeyUp, vkLControl, scLControl},
	} {
		kb := kbdllHookStruct{vkCode: k.vk, scanCode: k.scan}
		syscall.SyscallN(keyboardProc, hcAction, uintptr(k.msg), uintptr(unsafe.Pointer(&kb)))
	}
	const hcNoRemove = 3
	syscall.SyscallN(mouseProc, hcNoRemove, wmRButtonDown, uintptr(unsafe.Pointer(&mouse)))
	c := s.rec.tl.Clicks
	if len(c) != 1 || c[0].Button != ButtonLeft || c[0].X != 30 || c[0].Y != 40 {
		t.Fatalf("clicks %+v", c)
	}
	if got := labels(s); !reflect.DeepEqual(got, []string{"Ctrl+S"}) {
		t.Fatal(got)
	}
}
