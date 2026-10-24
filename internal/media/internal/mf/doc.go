// Package mf calls Media Foundation, Direct3D 11 and COM from Go without cgo: DLL exports
// through golang.org/x/sys/windows and interface methods through their vtables.
//
// Vtable slots and GUIDs come from the Windows SDK headers (mfobjects.h, mfreadwrite.h,
// mftransform.h, mfidl.h, mfapi.h, codecapi.h, d3d11.h); TestGUIDsMatchSDK rechecks them
// when the SDK is installed. The package is empty outside Windows.
package mf
