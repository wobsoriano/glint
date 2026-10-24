package mf

import (
	"errors"
	"syscall"
	"unsafe"
)

var procD3D11CreateDevice = d3d11.NewProc("D3D11CreateDevice")

// D3D11_TEXTURE2D_DESC.
type TextureDesc struct {
	Width, Height, MipLevels, ArraySize uint32
	Format                              uint32
	SampleCount, SampleQuality          uint32
	Usage, BindFlags, CPUAccessFlags    uint32
	MiscFlags                           uint32
}

const (
	D3D11_USAGE_STAGING   = 3
	D3D11_CPU_ACCESS_READ = 0x20000
	D3D11_MAP_READ        = 1
)

// Device is a hardware Direct3D 11 device with its immediate context, shared with Media
// Foundation through a DXGI device manager.
type Device struct {
	device  Unknown // ID3D11Device
	context Unknown // ID3D11DeviceContext
	Manager Unknown // IMFDXGIDeviceManager
}

func CreateDevice() (*Device, error) {
	const (
		driverTypeHardware = 1
		createBGRASupport  = 0x20
		createVideoSupport = 0x800
		sdkVersion         = 7
	)
	levels := [2]uint32{0xb100, 0xb000}
	var device, context unsafe.Pointer
	r, _, _ := procD3D11CreateDevice.Call(0, driverTypeHardware, 0, createBGRASupport|createVideoSupport,
		uintptr(unsafe.Pointer(&levels[0])), uintptr(len(levels)), sdkVersion,
		uintptr(unsafe.Pointer(&device)), 0, uintptr(unsafe.Pointer(&context)))
	if err := check("D3D11CreateDevice", r); err != nil {
		return nil, err
	}
	d := &Device{device: Unknown{device}, context: Unknown{context}}
	// Media Foundation calls into the device from its own threads.
	mt, err := d.context.QueryInterface(&IID_ID3D11Multithread)
	if err != nil {
		d.Release()
		return nil, err
	}
	syscall.SyscallN(mt.fn(5), mt.this(), 1)
	mt.Release()

	var token uint32
	var manager unsafe.Pointer
	r, _, _ = procMFCreateDXGIDeviceManager.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&manager)))
	if err := check("MFCreateDXGIDeviceManager", r); err != nil {
		d.Release()
		return nil, err
	}
	d.Manager = Unknown{manager}
	r, _, _ = syscall.SyscallN(d.Manager.fn(7), d.Manager.this(), d.device.this(), uintptr(token))
	if err := check("IMFDXGIDeviceManager::ResetDevice", r); err != nil {
		d.Release()
		return nil, err
	}
	return d, nil
}

func (d *Device) Release() {
	d.Manager.Release()
	d.context.Release()
	d.device.Release()
}

// Texture2D is ID3D11Texture2D.
type Texture2D struct{ Unknown }

func (t Texture2D) Desc() TextureDesc {
	var desc TextureDesc
	syscall.SyscallN(t.fn(10), t.this(), uintptr(unsafe.Pointer(&desc)))
	return desc
}

// StagingTexture makes a CPU-readable texture shaped like desc.
func (d *Device) StagingTexture(desc TextureDesc) (Texture2D, error) {
	desc.MipLevels, desc.ArraySize = 1, 1
	desc.Usage, desc.BindFlags, desc.CPUAccessFlags, desc.MiscFlags = D3D11_USAGE_STAGING, 0, D3D11_CPU_ACCESS_READ, 0
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(d.device.fn(5), d.device.this(), uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&out)))
	if err := check("ID3D11Device::CreateTexture2D", r); err != nil {
		return Texture2D{}, err
	}
	if out == nil {
		return Texture2D{}, errors.New("ID3D11Device::CreateTexture2D returned nothing")
	}
	return Texture2D{Unknown{out}}, nil
}

// CopySubresource copies one subresource of src into subresource 0 of dst.
func (d *Device) CopySubresource(dst, src Texture2D, srcIndex uint32) {
	syscall.SyscallN(d.context.fn(46), d.context.this(), dst.this(), 0, 0, 0, 0, src.this(), uintptr(srcIndex), 0)
}

// Map maps subresource 0 of a staging texture for reading until Unmap.
func (d *Device) Map(t Texture2D) (data unsafe.Pointer, rowPitch uint32, err error) {
	var mapped struct {
		Data                 unsafe.Pointer
		RowPitch, DepthPitch uint32
	}
	r, _, _ := syscall.SyscallN(d.context.fn(14), d.context.this(), t.this(), 0, D3D11_MAP_READ, 0, uintptr(unsafe.Pointer(&mapped)))
	return mapped.Data, mapped.RowPitch, check("ID3D11DeviceContext::Map", r)
}

func (d *Device) Unmap(t Texture2D) {
	syscall.SyscallN(d.context.fn(15), d.context.this(), t.this(), 0)
}
