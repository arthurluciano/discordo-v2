//! Playback of a received H.264 stream: decoded on the GPU and drawn into a swap chain that the
//! app shows (a WinUI SwapChainPanel). The picture never leaves GPU memory.

use std::mem::ManuallyDrop;

use windows::Win32::Graphics::Direct3D11::ID3D11Texture2D;
use windows::Win32::Graphics::Dxgi::Common::{DXGI_ALPHA_MODE_IGNORE, DXGI_FORMAT_B8G8R8A8_UNORM, DXGI_SAMPLE_DESC};
use windows::Win32::Graphics::Dxgi::{
    DXGI_PRESENT, DXGI_SCALING_STRETCH, DXGI_SWAP_CHAIN_DESC1, DXGI_SWAP_CHAIN_FLAG, DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
    DXGI_USAGE_RENDER_TARGET_OUTPUT, IDXGIDevice, IDXGIFactory2, IDXGISwapChain1,
};
use windows::Win32::Media::MediaFoundation::*;
use windows::Win32::System::Com::{CLSCTX_INPROC_SERVER, CoCreateInstance};
use windows::core::{Error, Interface, Result};

use super::gpu::{Converter, Gpu};
use crate::h264;

pub struct Player {
    gpu: Gpu,
    decoder: IMFTransform,
    swap_chain: IDXGISwapChain1,
    converter: Option<Converter>,
    /// The visible picture size, once the decoder has told us.
    size: (u32, u32),
    /// After a loss, nothing can be decoded until the next keyframe.
    waiting_for_keyframe: bool,
    frames: i64,
}

#[derive(Default)]
pub struct Pushed {
    /// The picture size changed (also reported for the first frame).
    pub resized: Option<(u32, u32)>,
    /// The frame could not be used; ask the sender for a keyframe.
    pub need_keyframe: bool,
}

impl Player {
    pub fn new() -> Result<Self> {
        unsafe {
            let gpu = Gpu::new()?;
            let decoder: IMFTransform = CoCreateInstance(&CLSID_MSH264DecoderMFT, None, CLSCTX_INPROC_SERVER)?;
            if let Ok(attributes) = decoder.GetAttributes() {
                let _ = attributes.SetUINT32(&CODECAPI_AVLowLatencyMode, 1); // output each frame as soon as it is decoded
            }
            // With the device manager the decoder runs on the GPU (DXVA) and outputs textures.
            decoder.ProcessMessage(MFT_MESSAGE_SET_D3D_MANAGER, gpu.manager.as_raw() as usize)?;

            let input = MFCreateMediaType()?;
            input.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
            input.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_H264)?;
            decoder.SetInputType(0, &input, 0)?;
            pick_output(&decoder)?;
            decoder.ProcessMessage(MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0)?;

            // The real size comes with the first frame; the swap chain is resized then.
            let desc = DXGI_SWAP_CHAIN_DESC1 {
                Width: 1280,
                Height: 720,
                Format: DXGI_FORMAT_B8G8R8A8_UNORM,
                SampleDesc: DXGI_SAMPLE_DESC { Count: 1, Quality: 0 },
                BufferUsage: DXGI_USAGE_RENDER_TARGET_OUTPUT,
                BufferCount: 2,
                Scaling: DXGI_SCALING_STRETCH,
                SwapEffect: DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
                AlphaMode: DXGI_ALPHA_MODE_IGNORE,
                ..Default::default()
            };
            let factory: IDXGIFactory2 = gpu.device.cast::<IDXGIDevice>()?.GetAdapter()?.GetParent()?;
            let swap_chain = factory.CreateSwapChainForComposition(&gpu.device, &desc, None)?;

            Ok(Self { gpu, decoder, swap_chain, converter: None, size: (0, 0), waiting_for_keyframe: true, frames: 0 })
        }
    }

    pub fn swap_chain(&self) -> &IDXGISwapChain1 {
        &self.swap_chain
    }

    /// Decodes and shows one access unit. `contiguous` is false when frames before it were lost.
    pub fn push(&mut self, data: &[u8], contiguous: bool) -> Result<Pushed> {
        let mut pushed = Pushed::default();
        self.waiting_for_keyframe |= !contiguous;
        if self.waiting_for_keyframe {
            if !h264::is_keyframe(data) {
                pushed.need_keyframe = true;
                return Ok(pushed);
            }
            self.waiting_for_keyframe = false;
        }
        unsafe {
            let buffer = MFCreateMemoryBuffer(data.len() as u32)?;
            let mut ptr = std::ptr::null_mut();
            buffer.Lock(&mut ptr, None, None)?;
            std::ptr::copy_nonoverlapping(data.as_ptr(), ptr, data.len());
            buffer.Unlock()?;
            buffer.SetCurrentLength(data.len() as u32)?;
            let sample = MFCreateSample()?;
            sample.AddBuffer(&buffer)?;
            sample.SetSampleTime(self.frames)?; // only has to increase; presentation is "as soon as decoded"
            self.frames += 1;

            self.decoder.ProcessInput(0, &sample, 0)?;
            loop {
                let mut buffers = [MFT_OUTPUT_DATA_BUFFER::default()];
                let mut status = 0;
                let result = self.decoder.ProcessOutput(0, &mut buffers, &mut status);
                let frame = ManuallyDrop::take(&mut buffers[0].pSample);
                ManuallyDrop::drop(&mut buffers[0].pEvents);
                match result {
                    Ok(()) => self.show(&frame.ok_or_else(Error::empty)?)?,
                    Err(e) if e.code() == MF_E_TRANSFORM_NEED_MORE_INPUT => break,
                    Err(e) if e.code() == MF_E_TRANSFORM_STREAM_CHANGE => {
                        let size = pick_output(&self.decoder)?;
                        if size != self.size {
                            self.resize(size)?;
                            pushed.resized = Some(size);
                        }
                    }
                    Err(e) => return Err(e),
                }
            }
        }
        Ok(pushed)
    }

    unsafe fn resize(&mut self, size: (u32, u32)) -> Result<()> {
        unsafe {
            self.converter = None;
            self.swap_chain.ResizeBuffers(2, size.0, size.1, DXGI_FORMAT_B8G8R8A8_UNORM, DXGI_SWAP_CHAIN_FLAG(0))?;
            self.converter = Some(Converter::new(&self.gpu, size, size, 60)?);
            self.size = size;
            Ok(())
        }
    }

    /// Draws a decoded frame (an NV12 texture) into the swap chain and presents it.
    unsafe fn show(&mut self, frame: &IMFSample) -> Result<()> {
        unsafe {
            let Some(converter) = &self.converter else { return Ok(()) };
            let buffer: IMFDXGIBuffer = frame.GetBufferByIndex(0)?.cast()?;
            let mut raw = std::ptr::null_mut();
            buffer.GetResource(&ID3D11Texture2D::IID, &mut raw)?;
            let texture = ID3D11Texture2D::from_raw(raw);
            let back: ID3D11Texture2D = self.swap_chain.GetBuffer(0)?;
            converter.convert(&texture, buffer.GetSubresourceIndex()?, &back)?;
            self.swap_chain.Present(0, DXGI_PRESENT(0)).ok()
        }
    }
}

/// Selects NV12 output and returns the visible picture size.
unsafe fn pick_output(decoder: &IMFTransform) -> Result<(u32, u32)> {
    unsafe {
        for index in 0.. {
            let candidate = decoder.GetOutputAvailableType(0, index)?; // errors out when the list ends
            if candidate.GetGUID(&MF_MT_SUBTYPE)? != MFVideoFormat_NV12 {
                continue;
            }
            decoder.SetOutputType(0, &candidate, 0)?;
            // The coded size is padded to a multiple of 16; the display aperture is the real picture.
            let mut area = [0u8; 16]; // MFVideoArea: two 4-byte offsets, then width and height
            if candidate.GetBlob(&MF_MT_MINIMUM_DISPLAY_APERTURE, &mut area, None).is_ok() {
                let field = |at: usize| u32::from_le_bytes([area[at], area[at + 1], area[at + 2], area[at + 3]]);
                if field(8) > 0 && field(12) > 0 {
                    return Ok((field(8), field(12)));
                }
            }
            let packed = candidate.GetUINT64(&MF_MT_FRAME_SIZE)?;
            return Ok(((packed >> 32) as u32, packed as u32));
        }
        Err(Error::empty())
    }
}
