package mf

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	combase                    = windows.NewLazySystemDLL("combase.dll")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
)

// activationFactory is the WinRT class's iid factory. The caller's thread must be in an
// apartment, which Enter provides.
func activationFactory(class string, iid *GUID) (Unknown, error) {
	u, err := windows.UTF16FromString(class)
	if err != nil {
		return Unknown{}, err
	}
	var name uintptr
	r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&name)))
	if err := check("WindowsCreateString", r); err != nil {
		return Unknown{}, err
	}
	defer procWindowsDeleteString.Call(name)
	var out unsafe.Pointer
	r, _, _ = procRoGetActivationFactory.Call(name, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := check("RoGetActivationFactory "+class, r); err != nil {
		return Unknown{}, err
	}
	return Unknown{out}, nil
}

func nonNil(op string, p unsafe.Pointer, err error) (Unknown, error) {
	if err != nil {
		return Unknown{}, err
	}
	if p == nil {
		return Unknown{}, &Error{Op: op, HR: HRESULT(windows.E_POINTER)}
	}
	return Unknown{p}, nil
}
