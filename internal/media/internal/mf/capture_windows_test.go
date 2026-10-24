package mf

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestCaptureIIDsMatchSDK rereads the Windows.Graphics.Capture IIDs from the SDK headers.
func TestCaptureIIDsMatchSDK(t *testing.T) {
	roots, _ := filepath.Glob(`C:\Program Files (x86)\Windows Kits\10\Include\10.*`)
	if len(roots) == 0 {
		t.Skip("Windows SDK headers are not installed")
	}
	root := roots[len(roots)-1]
	var src strings.Builder
	for _, f := range []string{
		`winrt\windows.graphics.capture.h`, `um\windows.graphics.capture.interop.h`,
		`um\windows.graphics.directx.direct3d11.interop.h`, `winrt\windows.graphics.directx.direct3d11.h`,
		`winrt\windows.foundation.h`, `shared\dxgi.h`,
	} {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	h := src.String()
	quoted := map[string]*GUID{
		"IGraphicsCaptureItemInterop":         &IID_IGraphicsCaptureItemInterop,
		"IDirect3DDxgiInterfaceAccess":        &IID_IDirect3DDxgiInterfaceAccess,
		"IGraphicsCaptureItem":                &IID_IGraphicsCaptureItem,
		"IDirect3D11CaptureFramePoolStatics2": &IID_IDirect3D11CaptureFramePoolStatics2,
		"IGraphicsCaptureSession2":            &IID_IGraphicsCaptureSession2,
		"IGraphicsCaptureSession3":            &IID_IGraphicsCaptureSession3,
		"IClosable":                           &IID_IClosable,
		"IDirect3DDevice":                     &IID_IDirect3DDevice,
		"IDXGIDevice":                         &IID_IDXGIDevice,
	}
	for name, got := range quoted {
		want, err := sdkInterfaceGUID(h, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if *got != want {
			t.Errorf("%s is %v here, %v in the SDK", name, *got, want)
		}
	}
}

// sdkInterfaceGUID finds name's IID in MIDL_INTERFACE, DECLARE_INTERFACE_IID_ or
// __declspec(uuid) form.
func sdkInterfaceGUID(h, name string) (GUID, error) {
	for _, p := range []string{
		`DECLARE_INTERFACE_IID_\(\s*` + name + `\s*,\s*\w+\s*,\s*"([0-9A-Fa-f-]{36})"`,
		`__declspec\(uuid\("([0-9A-Fa-f-]{36})"\)\)\s*` + name + `\b`,
	} {
		if m := regexp.MustCompile(p).FindStringSubmatch(h); m != nil {
			return parseGUIDString(m[1])
		}
	}
	return sdkGUID(h, name)
}

// TestCaptureMonitorFrame captures one frame of the primary monitor with the cursor left
// out, and reads its pixels back.
func TestCaptureMonitorFrame(t *testing.T) {
	if testing.Short() {
		t.Skip("needs an interactive desktop")
	}
	if err := Startup(); err != nil {
		t.Fatal(err)
	}
	leave, err := Enter()
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	d, err := CreateDevice()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Release()
	monitor, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("MonitorFromPoint").Call(0, 1)
	item, err := CaptureMonitor(monitor)
	if err != nil {
		t.Fatal(err)
	}
	defer item.Release()
	size, err := item.Size()
	if err != nil {
		t.Fatal(err)
	}
	if size.W < 640 || size.H < 480 {
		t.Fatalf("monitor item is %dx%d", size.W, size.H)
	}
	pool, err := d.FramePool(size)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	session, err := pool.Session(item)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.SetCursorCapture(false); err != nil {
		t.Fatal(err)
	}
	if err := session.SetBorderRequired(false); err != nil {
		t.Logf("border stays: %v", err)
	}
	if err := session.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var frame CaptureFrame
	for {
		f, ok, err := pool.NextFrame()
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			frame = f
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no frame within 3s")
		}
		time.Sleep(2 * time.Millisecond)
	}
	defer frame.Close()
	content, err := frame.ContentSize()
	if err != nil {
		t.Fatal(err)
	}
	if content != size {
		t.Errorf("content %v, item %v", content, size)
	}
	tex, err := frame.Texture()
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Release()
	desc := tex.Desc()
	if int32(desc.Width) < size.W || desc.Format != DXGI_FORMAT_B8G8R8A8_UNORM {
		t.Fatalf("frame texture %+v", desc)
	}
	latest, err := d.Texture(64, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer latest.Release()
	d.CopyRegion(latest, 0, 0, tex, Box{Left: 10, Top: 10, Right: 74, Bottom: 42, Back: 1})
	staging, err := d.StagingTexture(latest.Desc())
	if err != nil {
		t.Fatal(err)
	}
	defer staging.Release()
	d.CopyResource(staging, latest)
	data, pitch, err := d.Map(staging)
	if err != nil {
		t.Fatal(err)
	}
	px := unsafe.Slice((*byte)(data), int(pitch)*32)
	opaque := px[3] == 255
	d.Unmap(staging)
	if pitch < 64*4 || !opaque {
		t.Errorf("pitch %d, first pixel alpha %d", pitch, px[3])
	}
}
