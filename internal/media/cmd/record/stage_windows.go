package main

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procDwmGetCompositionTimingInfo = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetCompositionTimingInfo")
	procQueryPerformanceCounter     = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryPerformanceCounter")
	procQueryPerformanceFrequency   = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryPerformanceFrequency")
)

// untilVBlank is how long until the next vertical blank, when the desktop shows what DWM
// composed last.
func untilVBlank() (time.Duration, bool) {
	// DWM_TIMING_INFO is byte-packed: qpcRefreshPeriod and qpcVBlank sit at offsets 12 and
	// 28 of its 292 bytes.
	var info [292]byte
	*(*uint32)(unsafe.Pointer(&info[0])) = uint32(len(info))
	if hr, _, _ := procDwmGetCompositionTimingInfo.Call(0, uintptr(unsafe.Pointer(&info[0]))); hr != 0 {
		return 0, false
	}
	var now, freq int64
	procQueryPerformanceCounter.Call(uintptr(unsafe.Pointer(&now)))
	procQueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&freq)))
	period, vblank := *(*int64)(unsafe.Pointer(&info[12])), *(*int64)(unsafe.Pointer(&info[28]))
	for period > 0 && vblank < now {
		vblank += period
	}
	return time.Duration(float64(vblank-now) / float64(freq) * float64(time.Second)), true
}
