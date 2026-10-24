package capture

import (
	"cmp"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	pkeyDeviceFriendlyName  = propertyKey{windows.GUID{Data1: 0xA45C254E, Data2: 0xDF1C, Data3: 0x4EFD, Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}}, 14}
)

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

const (
	eRender  = 0
	eCapture = 1
	eConsole = 0

	deviceStateActive = 1
	stgmRead          = 0
)

// IMMDeviceEnumerator, IMMDeviceCollection, IMMDevice and IPropertyStore vtable slots.
const (
	enumEnumAudioEndpoints      = 3
	enumGetDefaultAudioEndpoint = 4
	enumGetDevice               = 5
	collGetCount                = 3
	collItem                    = 4
	devActivate                 = 3
	devOpenPropertyStore        = 4
	devGetID                    = 5
	propsGetValue               = 5
)

// Microphones lists the active microphones, the default one first.
func Microphones() ([]AudioDevice, error) {
	var out []AudioDevice
	err := withCOM(func() error {
		en, err := coCreate(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
		if err != nil {
			return err
		}
		defer en.release()
		var defaultID DeviceID
		if d, err := defaultEndpoint(en, eCapture); err == nil {
			defaultID, _ = endpointID(d)
			d.release()
		}
		var coll *comObject
		if err := en.call(enumEnumAudioEndpoints, eCapture, deviceStateActive, uintptr(unsafe.Pointer(&coll))).err("EnumAudioEndpoints"); err != nil {
			return err
		}
		defer coll.release()
		var n uint32
		if err := coll.call(collGetCount, uintptr(unsafe.Pointer(&n))).err("IMMDeviceCollection::GetCount"); err != nil {
			return err
		}
		for i := range n {
			var d *comObject
			if err := coll.call(collItem, uintptr(i), uintptr(unsafe.Pointer(&d))).err("IMMDeviceCollection::Item"); err != nil {
				return err
			}
			info, err := describe(d)
			d.release()
			if err != nil {
				return err
			}
			out = append(out, info)
		}
		slices.SortStableFunc(out, func(a, b AudioDevice) int {
			return cmp.Compare(boolRank(a.ID != defaultID), boolRank(b.ID != defaultID))
		})
		return nil
	})
	return out, err
}

// DefaultOutput is the render endpoint that loopback records.
func DefaultOutput() (AudioDevice, error) {
	var out AudioDevice
	err := withCOM(func() error {
		en, err := coCreate(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
		if err != nil {
			return err
		}
		defer en.release()
		d, err := defaultEndpoint(en, eRender)
		if err != nil {
			return err
		}
		defer d.release()
		out, err = describe(d)
		return err
	})
	return out, err
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func defaultEndpoint(en *comObject, flow uintptr) (*comObject, error) {
	var d *comObject
	if err := en.call(enumGetDefaultAudioEndpoint, flow, eConsole, uintptr(unsafe.Pointer(&d))).err("GetDefaultAudioEndpoint"); err != nil {
		return nil, err
	}
	return d, nil
}

// openEndpoint resolves a Source to its IMMDevice.
func openEndpoint(en *comObject, src Source) (*comObject, error) {
	switch {
	case src.loopback:
		return defaultEndpoint(en, eRender)
	case src.mic == "":
		return defaultEndpoint(en, eCapture)
	}
	id, err := windows.UTF16PtrFromString(string(src.mic))
	if err != nil {
		return nil, err
	}
	var d *comObject
	if err := en.call(enumGetDevice, uintptr(unsafe.Pointer(id)), uintptr(unsafe.Pointer(&d))).err("microphone " + string(src.mic) + " not found"); err != nil {
		return nil, err
	}
	return d, nil
}

func endpointID(d *comObject) (DeviceID, error) {
	var p *uint16
	if err := d.call(devGetID, uintptr(unsafe.Pointer(&p))).err("IMMDevice::GetId"); err != nil {
		return "", err
	}
	return DeviceID(takeString(p)), nil
}

func describe(d *comObject) (AudioDevice, error) {
	id, err := endpointID(d)
	if err != nil {
		return AudioDevice{}, err
	}
	var store *comObject
	if err := d.call(devOpenPropertyStore, stgmRead, uintptr(unsafe.Pointer(&store))).err("OpenPropertyStore"); err != nil {
		return AudioDevice{}, err
	}
	defer store.release()
	var v propVariant
	if err := store.call(propsGetValue, uintptr(unsafe.Pointer(&pkeyDeviceFriendlyName)), uintptr(unsafe.Pointer(&v))).err("IPropertyStore::GetValue"); err != nil {
		return AudioDevice{}, err
	}
	defer v.clear()
	const vtLPWSTR = 31
	var name string
	if v.vt == vtLPWSTR && v.str != nil {
		name = windows.UTF16PtrToString(v.str)
	}
	return AudioDevice{ID: id, Name: name}, nil
}
