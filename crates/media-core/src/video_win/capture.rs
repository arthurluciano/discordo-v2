//! Screen capture with Windows.Graphics.Capture: frames arrive as GPU textures, never copied to RAM.

use windows::Graphics::Capture::{Direct3D11CaptureFrame, Direct3D11CaptureFramePool, GraphicsCaptureItem, GraphicsCaptureSession};
use windows::Graphics::DirectX::Direct3D11::IDirect3DDevice;
use windows::Graphics::DirectX::DirectXPixelFormat;
use windows::Win32::Foundation::POINT;
use windows::Win32::Graphics::Direct3D11::ID3D11Texture2D;
use windows::Win32::Graphics::Dxgi::IDXGIDevice;
use windows::Win32::Graphics::Gdi::{MONITOR_DEFAULTTOPRIMARY, MonitorFromPoint};
use windows::Win32::System::WinRT::Direct3D11::{CreateDirect3D11DeviceFromDXGIDevice, IDirect3DDxgiInterfaceAccess};
use windows::Win32::System::WinRT::Graphics::Capture::IGraphicsCaptureItemInterop;
use windows::core::{Interface, Result, factory};

use super::gpu::Gpu;

/// shortcut: always the primary monitor; add a picker (monitors and windows) to choose the source.
pub struct ScreenCapture {
    pool: Direct3D11CaptureFramePool,
    session: GraphicsCaptureSession,
    pub size: (u32, u32),
}

impl ScreenCapture {
    pub fn primary(gpu: &Gpu) -> Result<Self> {
        unsafe {
            let monitor = MonitorFromPoint(POINT::default(), MONITOR_DEFAULTTOPRIMARY);
            let interop = factory::<GraphicsCaptureItem, IGraphicsCaptureItemInterop>()?;
            let item: GraphicsCaptureItem = interop.CreateForMonitor(monitor)?;

            let dxgi: IDXGIDevice = gpu.device.cast()?;
            let device: IDirect3DDevice = CreateDirect3D11DeviceFromDXGIDevice(&dxgi)?.cast()?;
            let size = item.Size()?;
            // Free-threaded: we poll for frames from our own thread instead of needing a message loop.
            let pool = Direct3D11CaptureFramePool::CreateFreeThreaded(&device, DirectXPixelFormat::B8G8R8A8UIntNormalized, 2, size)?;
            let session = pool.CreateCaptureSession(&item)?;
            let _ = session.SetIsBorderRequired(false); // the yellow border; only removable on Windows 11
            session.StartCapture()?;
            Ok(Self { pool, session, size: (size.Width as u32, size.Height as u32) })
        }
    }

    /// The newest frame, if the screen changed since the last call. Keep the returned frame alive
    /// while using its texture: dropping it hands the texture back to the pool.
    pub fn latest(&self) -> Option<(Direct3D11CaptureFrame, ID3D11Texture2D)> {
        let mut newest = None;
        while let Ok(frame) = self.pool.TryGetNextFrame() {
            newest = Some(frame);
        }
        let frame = newest?;
        let access: IDirect3DDxgiInterfaceAccess = frame.Surface().ok()?.cast().ok()?;
        let texture = unsafe { access.GetInterface::<ID3D11Texture2D>().ok()? };
        Some((frame, texture))
    }
}

impl Drop for ScreenCapture {
    fn drop(&mut self) {
        let _ = self.session.Close();
        let _ = self.pool.Close();
    }
}
