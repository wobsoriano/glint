use anyhow::Context;
use windows::core::Interface;
use windows::Graphics::DirectX::Direct3D11::IDirect3DDevice;
use windows::Win32::Foundation::HMODULE;
use windows::Win32::Graphics::Direct3D::{D3D_DRIVER_TYPE_HARDWARE, D3D_FEATURE_LEVEL_11_0, D3D_FEATURE_LEVEL_11_1};
use windows::Win32::Graphics::Direct3D11::{
    D3D11CreateDevice, ID3D11Device, ID3D11DeviceContext, ID3D11Multithread, ID3D11Texture2D, D3D11_BIND_FLAG,
    D3D11_CPU_ACCESS_FLAG, D3D11_CREATE_DEVICE_BGRA_SUPPORT, D3D11_CREATE_DEVICE_VIDEO_SUPPORT, D3D11_SDK_VERSION,
    D3D11_TEXTURE2D_DESC, D3D11_USAGE,
};
use windows::Win32::Graphics::Dxgi::Common::{DXGI_FORMAT_B8G8R8A8_UNORM, DXGI_SAMPLE_DESC};
use windows::Win32::Graphics::Dxgi::IDXGIDevice;
use windows::Win32::Media::MediaFoundation::{IMFDXGIDeviceManager, MFCreateDXGIDeviceManager};
use windows::Win32::System::WinRT::Direct3D11::CreateDirect3D11DeviceFromDXGIDevice;

pub(crate) struct D3dDevice {
    pub device: ID3D11Device,
    pub context: ID3D11DeviceContext,
    pub manager: IMFDXGIDeviceManager,
}

impl D3dDevice {
    pub fn new() -> anyhow::Result<Self> {
        let mut device = None;
        let mut context = None;
        unsafe {
            D3D11CreateDevice(
                None,
                D3D_DRIVER_TYPE_HARDWARE,
                HMODULE::default(),
                D3D11_CREATE_DEVICE_BGRA_SUPPORT | D3D11_CREATE_DEVICE_VIDEO_SUPPORT,
                Some(&[D3D_FEATURE_LEVEL_11_1, D3D_FEATURE_LEVEL_11_0]),
                D3D11_SDK_VERSION,
                Some(&mut device),
                None,
                Some(&mut context),
            )
            .context("D3D11CreateDevice")?;
        }
        let device = device.context("D3D11CreateDevice returned no device")?;
        let context = context.context("D3D11CreateDevice returned no context")?;
        // Media Foundation calls into the device from its own threads.
        let mt: ID3D11Multithread = context.cast().context("ID3D11Multithread")?;
        // Returns the previous setting, not a status.
        let _ = unsafe { mt.SetMultithreadProtected(true) };
        let mut token = 0u32;
        let mut manager = None;
        unsafe { MFCreateDXGIDeviceManager(&mut token, &mut manager).context("MFCreateDXGIDeviceManager")? };
        let manager = manager.context("MFCreateDXGIDeviceManager returned nothing")?;
        unsafe { manager.ResetDevice(&device, token).context("IMFDXGIDeviceManager::ResetDevice")? };
        Ok(Self { device, context, manager })
    }

    pub fn winrt_device(&self) -> anyhow::Result<IDirect3DDevice> {
        let dxgi: IDXGIDevice = self.device.cast().context("IDXGIDevice")?;
        let inspectable = unsafe { CreateDirect3D11DeviceFromDXGIDevice(&dxgi).context("CreateDirect3D11DeviceFromDXGIDevice")? };
        inspectable.cast().context("IDirect3DDevice")
    }

    pub fn texture(
        &self,
        width: u32,
        height: u32,
        usage: D3D11_USAGE,
        bind: D3D11_BIND_FLAG,
        cpu_access: D3D11_CPU_ACCESS_FLAG,
    ) -> anyhow::Result<ID3D11Texture2D> {
        let desc = D3D11_TEXTURE2D_DESC {
            Width: width,
            Height: height,
            MipLevels: 1,
            ArraySize: 1,
            Format: DXGI_FORMAT_B8G8R8A8_UNORM,
            SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
            Usage: usage,
            BindFlags: bind.0 as u32,
            CPUAccessFlags: cpu_access.0 as u32,
            MiscFlags: 0,
        };
        let mut tex = None;
        unsafe { self.device.CreateTexture2D(&desc, None, Some(&mut tex)).context("CreateTexture2D")? };
        tex.context("CreateTexture2D returned nothing")
    }
}
