package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                          = windows.NewLazySystemDLL("user32.dll")
	procGetDisplayConfigBufferSizes = user32.NewProc("GetDisplayConfigBufferSizes")
	procQueryDisplayConfig          = user32.NewProc("QueryDisplayConfig")
	procDisplayConfigGetDeviceInfo  = user32.NewProc("DisplayConfigGetDeviceInfo")
)

type deviceInfoHeader struct {
	Type      uint32
	Size      uint32
	AdapterID windows.LUID
	ID        uint32
}

// advancedColour says, per active display path, whether Windows runs it in advanced colour
// (HDR), so the capture colour check is known to have run on such a desktop.
func advancedColour() []string {
	const (
		qdcOnlyActivePaths     = 2
		getSourceName          = 1
		getAdvancedColourInfo  = 9
		pathSize, modeSize     = 72, 64
		targetOffset, idOffset = 20, 28
	)
	var np, nm uint32
	if r, _, _ := procGetDisplayConfigBufferSizes.Call(qdcOnlyActivePaths, uintptr(unsafe.Pointer(&np)), uintptr(unsafe.Pointer(&nm))); r != 0 || np == 0 {
		return []string{fmt.Sprintf("advanced colour: unknown (GetDisplayConfigBufferSizes %d)", r)}
	}
	paths := make([]byte, int(np)*pathSize)
	modes := make([]byte, int(nm)*modeSize)
	if r, _, _ := procQueryDisplayConfig.Call(qdcOnlyActivePaths, uintptr(unsafe.Pointer(&np)), uintptr(unsafe.Pointer(&paths[0])), uintptr(unsafe.Pointer(&nm)), uintptr(unsafe.Pointer(&modes[0])), 0); r != 0 {
		return []string{fmt.Sprintf("advanced colour: unknown (QueryDisplayConfig %d)", r)}
	}
	var out []string
	for i := range int(np) {
		p := paths[i*pathSize:]
		source := struct {
			deviceInfoHeader
			Name [32]uint16
		}{deviceInfoHeader: deviceInfoHeader{
			Type: getSourceName, AdapterID: *(*windows.LUID)(unsafe.Pointer(&p[0])), ID: *(*uint32)(unsafe.Pointer(&p[8])),
		}}
		source.Size = uint32(unsafe.Sizeof(source))
		procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&source)))
		info := struct {
			deviceInfoHeader
			Value, Encoding, Bits uint32
		}{deviceInfoHeader: deviceInfoHeader{
			Type: getAdvancedColourInfo, AdapterID: *(*windows.LUID)(unsafe.Pointer(&p[targetOffset])), ID: *(*uint32)(unsafe.Pointer(&p[idOffset])),
		}}
		info.Size = uint32(unsafe.Sizeof(info))
		if r, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&info))); r != 0 {
			out = append(out, fmt.Sprintf("%s advanced colour: unknown (%d)", windows.UTF16ToString(source.Name[:]), r))
			continue
		}
		out = append(out, fmt.Sprintf("%s advanced colour: supported=%v enabled=%v bits=%d",
			windows.UTF16ToString(source.Name[:]), info.Value&1 != 0, info.Value&2 != 0, info.Bits))
	}
	return out
}
