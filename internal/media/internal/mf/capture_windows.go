package mf

import (
	"syscall"
	"unsafe"
)

// Windows.Graphics.Capture through its WinRT ABI. Interfaces derive from IInspectable,
// whose six methods take slots 0 to 5; IIDs and slot order come from the SDK's
// windows.graphics.capture.h, windows.graphics.capture.interop.h and
// windows.graphics.directx.direct3d11.interop.h.
var (
	IID_IGraphicsCaptureItemInterop          = newGUID(0x3628e81b, 0x3cac, 0x4c60, [8]byte{0xb7, 0xf4, 0x23, 0xce, 0x0e, 0x0c, 0x33, 0x56})
	IID_IGraphicsCaptureItem                 = newGUID(0x79c3f95b, 0x31f7, 0x4ec2, [8]byte{0xa4, 0x64, 0x63, 0x2e, 0xf5, 0xd3, 0x07, 0x60})
	IID_IDirect3D11CaptureFramePoolStatics2  = newGUID(0x589b103f, 0x6bbc, 0x5df5, [8]byte{0xa9, 0x91, 0x02, 0xe2, 0x8b, 0x3b, 0x66, 0xd5})
	IID_IGraphicsCaptureSession2             = newGUID(0x2c39ae40, 0x7d2e, 0x5044, [8]byte{0x80, 0x4e, 0x8b, 0x67, 0x99, 0xd4, 0xcf, 0x9e})
	IID_IGraphicsCaptureSession3             = newGUID(0xf2cdd966, 0x22ae, 0x5ea1, [8]byte{0x95, 0x96, 0x3a, 0x28, 0x93, 0x44, 0xc3, 0xbe})
	IID_IClosable                            = newGUID(0x30d5a829, 0x7fa4, 0x4026, [8]byte{0x83, 0xbb, 0xd7, 0x5b, 0xae, 0x4e, 0xa9, 0x9e})
	IID_IDirect3DDxgiInterfaceAccess         = newGUID(0xa9b3d012, 0x3df2, 0x4ee3, [8]byte{0xb8, 0xd1, 0x86, 0x95, 0xf4, 0x57, 0xd3, 0xc1})
	IID_IDirect3DDevice                      = newGUID(0xa37624ab, 0x8d5f, 0x4650, [8]byte{0x9d, 0x3e, 0x9e, 0xae, 0x3d, 0x9b, 0xc6, 0x70})
	IID_IDXGIDevice                          = newGUID(0x54ec77fa, 0x1377, 0x44e6, [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c})
	procCreateDirect3D11DeviceFromDXGIDevice = d3d11.NewProc("CreateDirect3D11DeviceFromDXGIDevice")
)

const (
	DXGI_FORMAT_B8G8R8A8_UNORM = 87
	// DirectXPixelFormat_B8G8R8A8UIntNormalized has the same value as its DXGI format.
	pixelFormatBGRA8 = 87
	captureBuffers   = 2

	slotClosableClose = 6

	slotItemSize = 7

	slotPoolRecreate             = 6
	slotPoolTryGetNextFrame      = 7
	slotPoolCreateCaptureSession = 10

	slotPoolStaticsCreateFreeThreaded = 6

	slotFrameSurface            = 6
	slotFrameSystemRelativeTime = 7
	slotFrameContentSize        = 8

	slotSessionStartCapture         = 6
	slotSessionPutCursorCapture     = 7
	slotSessionPutIsBorderRequired  = 7
	slotInteropCreateForWindow      = 3
	slotInteropCreateForMonitor     = 4
	slotDxgiInterfaceAccessGetIface = 3
)

// Size is SizeInt32. Its 8 bytes travel in one register when passed by value.
type Size struct{ W, H int32 }

func (s Size) arg() uintptr { return uintptr(uint32(s.W)) | uintptr(uint32(s.H))<<32 }

func closeObject(u Unknown) error {
	c, err := u.QueryInterface(&IID_IClosable)
	if err != nil {
		return err
	}
	defer c.Release()
	r, _, _ := syscall.SyscallN(c.fn(slotClosableClose), c.this())
	return check("IClosable::Close", r)
}

// Box is D3D11_BOX for a 2D texture: [Left, Right) by [Top, Bottom).
type Box struct {
	Left, Top, Front, Right, Bottom, Back uint32
}

// Texture creates a GPU-only BGRA texture that copies can land in.
func (d *Device) Texture(w, h int) (Texture2D, error) {
	desc := TextureDesc{
		Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1,
		Format: DXGI_FORMAT_B8G8R8A8_UNORM, SampleCount: 1,
	}
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(d.device.fn(5), d.device.this(), uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&out)))
	t, err := nonNil("ID3D11Device::CreateTexture2D", out, check("ID3D11Device::CreateTexture2D", r))
	return Texture2D{t}, err
}

// CopyRegion copies box of src's subresource 0 to (x, y) of dst's.
func (d *Device) CopyRegion(dst Texture2D, x, y uint32, src Texture2D, box Box) {
	syscall.SyscallN(d.context.fn(46), d.context.this(), dst.this(), 0, uintptr(x), uintptr(y), 0, src.this(), 0, uintptr(unsafe.Pointer(&box)))
}

func (d *Device) CopyResource(dst, src Texture2D) {
	syscall.SyscallN(d.context.fn(47), d.context.this(), dst.this(), src.this())
}

// winrtDevice wraps the device as the Windows.Graphics.DirectX.Direct3D11.IDirect3DDevice
// the capture API takes.
func (d *Device) winrtDevice() (Unknown, error) {
	dxgi, err := d.device.QueryInterface(&IID_IDXGIDevice)
	if err != nil {
		return Unknown{}, err
	}
	defer dxgi.Release()
	var insp unsafe.Pointer
	r, _, _ := procCreateDirect3D11DeviceFromDXGIDevice.Call(dxgi.this(), uintptr(unsafe.Pointer(&insp)))
	u, err := nonNil("CreateDirect3D11DeviceFromDXGIDevice", insp, check("CreateDirect3D11DeviceFromDXGIDevice", r))
	if err != nil {
		return Unknown{}, err
	}
	defer u.Release()
	return u.QueryInterface(&IID_IDirect3DDevice)
}

// CaptureItem is a GraphicsCaptureItem: one monitor or one window.
type CaptureItem struct{ Unknown }

func captureItem(slot int, handle uintptr) (CaptureItem, error) {
	interop, err := activationFactory("Windows.Graphics.Capture.GraphicsCaptureItem", &IID_IGraphicsCaptureItemInterop)
	if err != nil {
		return CaptureItem{}, err
	}
	defer interop.Release()
	op := "IGraphicsCaptureItemInterop::CreateForMonitor"
	if slot == slotInteropCreateForWindow {
		op = "IGraphicsCaptureItemInterop::CreateForWindow"
	}
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(interop.fn(slot), interop.this(), handle, uintptr(unsafe.Pointer(&IID_IGraphicsCaptureItem)), uintptr(unsafe.Pointer(&out)))
	u, err := nonNil(op, out, check(op, r))
	return CaptureItem{u}, err
}

func CaptureMonitor(hmonitor uintptr) (CaptureItem, error) {
	return captureItem(slotInteropCreateForMonitor, hmonitor)
}

func CaptureWindow(hwnd uintptr) (CaptureItem, error) {
	return captureItem(slotInteropCreateForWindow, hwnd)
}

// Size is the item's current size in physical pixels.
func (c CaptureItem) Size() (Size, error) {
	var s Size
	r, _, _ := syscall.SyscallN(c.fn(slotItemSize), c.this(), uintptr(unsafe.Pointer(&s)))
	return s, check("IGraphicsCaptureItem::get_Size", r)
}

// FramePool is a free-threaded Direct3D11CaptureFramePool of BGRA frames, polled with
// TryGetNextFrame instead of waiting for FrameArrived on a dispatcher.
type FramePool struct {
	Unknown
	device Unknown // IDirect3DDevice
}

func (d *Device) FramePool(size Size) (*FramePool, error) {
	dev, err := d.winrtDevice()
	if err != nil {
		return nil, err
	}
	statics, err := activationFactory("Windows.Graphics.Capture.Direct3D11CaptureFramePool", &IID_IDirect3D11CaptureFramePoolStatics2)
	if err != nil {
		dev.Release()
		return nil, err
	}
	defer statics.Release()
	const op = "Direct3D11CaptureFramePool::CreateFreeThreaded"
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(statics.fn(slotPoolStaticsCreateFreeThreaded), statics.this(), dev.this(), pixelFormatBGRA8, captureBuffers, size.arg(), uintptr(unsafe.Pointer(&out)))
	u, err := nonNil(op, out, check(op, r))
	if err != nil {
		dev.Release()
		return nil, err
	}
	return &FramePool{Unknown: u, device: dev}, nil
}

// Recreate resizes the pool's buffers, as the capture API asks when content changes size.
func (p *FramePool) Recreate(size Size) error {
	r, _, _ := syscall.SyscallN(p.fn(slotPoolRecreate), p.this(), p.device.this(), pixelFormatBGRA8, captureBuffers, size.arg())
	return check("IDirect3D11CaptureFramePool::Recreate", r)
}

// NextFrame is the oldest frame not yet taken, or ok=false when none is waiting.
func (p *FramePool) NextFrame() (f CaptureFrame, ok bool, err error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(p.fn(slotPoolTryGetNextFrame), p.this(), uintptr(unsafe.Pointer(&out)))
	if err := check("IDirect3D11CaptureFramePool::TryGetNextFrame", r); err != nil {
		return CaptureFrame{}, false, err
	}
	return CaptureFrame{Unknown{out}}, out != nil, nil
}

func (p *FramePool) Session(item CaptureItem) (CaptureSession, error) {
	const op = "IDirect3D11CaptureFramePool::CreateCaptureSession"
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(p.fn(slotPoolCreateCaptureSession), p.this(), item.this(), uintptr(unsafe.Pointer(&out)))
	u, err := nonNil(op, out, check(op, r))
	return CaptureSession{u}, err
}

func (p *FramePool) Close() {
	closeObject(p.Unknown)
	p.Release()
	p.device.Release()
}

// CaptureFrame is a Direct3D11CaptureFrame. Close returns its buffer to the pool.
type CaptureFrame struct{ Unknown }

func (f CaptureFrame) ContentSize() (Size, error) {
	var s Size
	r, _, _ := syscall.SyscallN(f.fn(slotFrameContentSize), f.this(), uintptr(unsafe.Pointer(&s)))
	return s, check("IDirect3D11CaptureFrame::get_ContentSize", r)
}

// SystemRelativeTime is when the frame is shown, the vertical blank it reaches the screen
// at, on the performance counter in 100 ns units. Frames usually arrive before it.
func (f CaptureFrame) SystemRelativeTime() (int64, error) {
	var t int64
	r, _, _ := syscall.SyscallN(f.fn(slotFrameSystemRelativeTime), f.this(), uintptr(unsafe.Pointer(&t)))
	return t, check("IDirect3D11CaptureFrame::get_SystemRelativeTime", r)
}

// Texture is the frame's surface; release it before closing the frame.
func (f CaptureFrame) Texture() (Texture2D, error) {
	const op = "IDirect3D11CaptureFrame::get_Surface"
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(f.fn(slotFrameSurface), f.this(), uintptr(unsafe.Pointer(&out)))
	surface, err := nonNil(op, out, check(op, r))
	if err != nil {
		return Texture2D{}, err
	}
	defer surface.Release()
	access, err := surface.QueryInterface(&IID_IDirect3DDxgiInterfaceAccess)
	if err != nil {
		return Texture2D{}, err
	}
	defer access.Release()
	const getOp = "IDirect3DDxgiInterfaceAccess::GetInterface"
	var tex unsafe.Pointer
	r, _, _ = syscall.SyscallN(access.fn(slotDxgiInterfaceAccessGetIface), access.this(), uintptr(unsafe.Pointer(&IID_ID3D11Texture2D)), uintptr(unsafe.Pointer(&tex)))
	t, err := nonNil(getOp, tex, check(getOp, r))
	return Texture2D{t}, err
}

func (f CaptureFrame) Close() {
	closeObject(f.Unknown)
	f.Release()
}

// CaptureSession is a GraphicsCaptureSession.
type CaptureSession struct{ Unknown }

func (s CaptureSession) setFlag(iid *GUID, slot int, op string, v bool) error {
	u, err := s.QueryInterface(iid)
	if err != nil {
		return err
	}
	defer u.Release()
	r, _, _ := syscall.SyscallN(u.fn(slot), u.this(), uintptr(boolU8(v)))
	return check(op, r)
}

// SetCursorCapture says whether the cursor is drawn into frames (Windows 10 2004 and later).
func (s CaptureSession) SetCursorCapture(on bool) error {
	return s.setFlag(&IID_IGraphicsCaptureSession2, slotSessionPutCursorCapture, "IGraphicsCaptureSession2::put_IsCursorCaptureEnabled", on)
}

// SetBorderRequired says whether Windows outlines the captured item in yellow.
func (s CaptureSession) SetBorderRequired(on bool) error {
	return s.setFlag(&IID_IGraphicsCaptureSession3, slotSessionPutIsBorderRequired, "IGraphicsCaptureSession3::put_IsBorderRequired", on)
}

func (s CaptureSession) Start() error {
	r, _, _ := syscall.SyscallN(s.fn(slotSessionStartCapture), s.this())
	return check("IGraphicsCaptureSession::StartCapture", r)
}

func (s CaptureSession) Close() {
	closeObject(s.Unknown)
	s.Release()
}

func boolU8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
