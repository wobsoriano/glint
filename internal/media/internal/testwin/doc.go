// Package testwin opens plain Win32 windows of known content and position, so recorder
// tests and the record tool can find them in what they capture. Windows are created
// per-monitor DPI aware, so their rectangles are physical pixels whatever the process
// manifest says. Outside Windows the package is empty.
package testwin
