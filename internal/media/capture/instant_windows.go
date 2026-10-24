package capture

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	procQueryPerformanceCounter   = kernel32.NewProc("QueryPerformanceCounter")
	procQueryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")
	qpcFrequency                  = func() int64 {
		var f int64
		procQueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&f)))
		return f
	}()
)

// Now is the performance counter now.
func Now() Instant {
	var t int64
	procQueryPerformanceCounter.Call(uintptr(unsafe.Pointer(&t)))
	return Instant(t/qpcFrequency*1e7 + t%qpcFrequency*1e7/qpcFrequency)
}
