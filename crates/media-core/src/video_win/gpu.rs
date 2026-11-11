//! The Direct3D 11 device and the GPU color-space/scale conversion shared by capture and playback.

use std::mem::ManuallyDrop;
use std::sync::Once;

use windows::Win32::Foundation::{HMODULE, RECT};
use windows::Win32::Graphics::Direct3D::{D3D_DRIVER_TYPE_HARDWARE, D3D_DRIVER_TYPE_WARP};
use windows::Win32::Graphics::Direct3D11::*;
use windows::Win32::Graphics::Dxgi::Common::{DXGI_FORMAT, DXGI_RATIONAL, DXGI_SAMPLE_DESC};
use windows::Win32::Media::MediaFoundation::{IMFDXGIDeviceManager, MF_VERSION, MFCreateDXGIDeviceManager, MFStartup};
use windows::core::{Error, Interface, Result};

/// A device plus the Media Foundation manager that lets encoders and decoders work on its textures.
/// Each pipeline creates its own on its own thread, so nothing here needs to cross threads.
pub struct Gpu {
    pub device: ID3D11Device,
    pub context: ID3D11DeviceContext,
    pub manager: IMFDXGIDeviceManager,
}

impl Gpu {
    pub fn new() -> Result<Self> {
        unsafe {
            static MF: Once = Once::new();
            MF.call_once(|| {
                let _ = MFStartup(MF_VERSION, 0); // MFSTARTUP_FULL; lives as long as the process
            });

            let flags = D3D11_CREATE_DEVICE_BGRA_SUPPORT | D3D11_CREATE_DEVICE_VIDEO_SUPPORT;
            let (mut device, mut context) = (None, None);
            // WARP (software) only when there is no usable GPU at all.
            D3D11CreateDevice(None, D3D_DRIVER_TYPE_HARDWARE, HMODULE::default(), flags, None, D3D11_SDK_VERSION, Some(&mut device), None, Some(&mut context))
                .or_else(|_| {
                    D3D11CreateDevice(None, D3D_DRIVER_TYPE_WARP, HMODULE::default(), D3D11_CREATE_DEVICE_BGRA_SUPPORT, None, D3D11_SDK_VERSION, Some(&mut device), None, Some(&mut context))
                })?;
            let (device, context) = (device.ok_or_else(Error::empty)?, context.ok_or_else(Error::empty)?);
            // Media Foundation uses the device from its own threads.
            let _ = device.cast::<ID3D11Multithread>()?.SetMultithreadProtected(true);

            let (mut token, mut manager) = (0u32, None);
            MFCreateDXGIDeviceManager(&mut token, &mut manager)?;
            let manager = manager.ok_or_else(Error::empty)?;
            manager.ResetDevice(&device, token)?;
            Ok(Self { device, context, manager })
        }
    }

    /// A texture the video processor can render into and an encoder can read from.
    pub fn texture(&self, width: u32, height: u32, format: DXGI_FORMAT) -> Result<ID3D11Texture2D> {
        let desc = D3D11_TEXTURE2D_DESC {
            Width: width,
            Height: height,
            MipLevels: 1,
            ArraySize: 1,
            Format: format,
            SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
            Usage: D3D11_USAGE_DEFAULT,
            BindFlags: D3D11_BIND_RENDER_TARGET.0 as u32,
            CPUAccessFlags: 0,
            MiscFlags: 0,
        };
        let mut texture = None;
        unsafe { self.device.CreateTexture2D(&desc, None, Some(&mut texture))? };
        texture.ok_or_else(Error::empty)
    }
}

/// Scales and converts between pixel formats (BGRA <-> NV12) with the GPU's video processor.
pub struct Converter {
    device: ID3D11VideoDevice,
    context: ID3D11VideoContext,
    enumerator: ID3D11VideoProcessorEnumerator,
    processor: ID3D11VideoProcessor,
}

impl Converter {
    pub fn new(gpu: &Gpu, input: (u32, u32), output: (u32, u32), fps: u32) -> Result<Self> {
        unsafe {
            let device: ID3D11VideoDevice = gpu.device.cast()?;
            let context: ID3D11VideoContext = gpu.context.cast()?;
            let rate = DXGI_RATIONAL { Numerator: fps, Denominator: 1 };
            let desc = D3D11_VIDEO_PROCESSOR_CONTENT_DESC {
                InputFrameFormat: D3D11_VIDEO_FRAME_FORMAT_PROGRESSIVE,
                InputFrameRate: rate,
                InputWidth: input.0,
                InputHeight: input.1,
                OutputFrameRate: rate,
                OutputWidth: output.0,
                OutputHeight: output.1,
                Usage: D3D11_VIDEO_USAGE_PLAYBACK_NORMAL,
            };
            let enumerator = device.CreateVideoProcessorEnumerator(&desc)?;
            let processor = device.CreateVideoProcessor(&enumerator, 0)?;
            // Decoded frames are padded to a multiple of 16; only the visible part is the picture.
            let visible = RECT { left: 0, top: 0, right: input.0 as i32, bottom: input.1 as i32 };
            context.VideoProcessorSetStreamSourceRect(&processor, 0, true, Some(&visible));
            Ok(Self { device, context, enumerator, processor })
        }
    }

    /// Draws `input` (one slice of a texture array, for decoder output) onto all of `output`.
    pub fn convert(&self, input: &ID3D11Texture2D, slice: u32, output: &ID3D11Texture2D) -> Result<()> {
        unsafe {
            let out_desc = D3D11_VIDEO_PROCESSOR_OUTPUT_VIEW_DESC {
                ViewDimension: D3D11_VPOV_DIMENSION_TEXTURE2D,
                Anonymous: D3D11_VIDEO_PROCESSOR_OUTPUT_VIEW_DESC_0 { Texture2D: D3D11_TEX2D_VPOV { MipSlice: 0 } },
            };
            let mut out_view = None;
            self.device.CreateVideoProcessorOutputView(output, &self.enumerator, &out_desc, Some(&mut out_view))?;

            let in_desc = D3D11_VIDEO_PROCESSOR_INPUT_VIEW_DESC {
                FourCC: 0,
                ViewDimension: D3D11_VPIV_DIMENSION_TEXTURE2D,
                Anonymous: D3D11_VIDEO_PROCESSOR_INPUT_VIEW_DESC_0 { Texture2D: D3D11_TEX2D_VPIV { MipSlice: 0, ArraySlice: slice } },
            };
            let mut in_view = None;
            self.device.CreateVideoProcessorInputView(input, &self.enumerator, &in_desc, Some(&mut in_view))?;

            let mut stream = D3D11_VIDEO_PROCESSOR_STREAM { Enable: true.into(), pInputSurface: ManuallyDrop::new(in_view), ..Default::default() };
            let result = self.context.VideoProcessorBlt(&self.processor, out_view.as_ref().ok_or_else(Error::empty)?, 0, std::slice::from_ref(&stream));
            ManuallyDrop::drop(&mut stream.pInputSurface);
            result
        }
    }
}
