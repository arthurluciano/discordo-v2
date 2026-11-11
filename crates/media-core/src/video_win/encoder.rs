//! H.264 encoding through Media Foundation: the GPU vendor's encoder (NVENC, AMF, Quick Sync)
//! when there is one, Microsoft's software encoder otherwise.

use std::mem::ManuallyDrop;

use windows::Win32::Graphics::Direct3D11::ID3D11Texture2D;
use windows::Win32::Media::MediaFoundation::*;
use windows::Win32::System::Com::{CLSCTX_INPROC_SERVER, CoCreateInstance, CoTaskMemFree};
use windows::Win32::System::Variant::VARIANT;
use windows::core::{Error, GUID, Interface, Result};

use super::gpu::Gpu;
use crate::h264;

pub struct Encoder {
    transform: IMFTransform,
    /// Hardware encoders are asynchronous: they say when they want input and when output is ready.
    events: Option<IMFMediaEventGenerator>,
    codec: Option<ICodecAPI>,
    /// How many inputs the asynchronous encoder has asked for and not received yet.
    wanted: u32,
    frame_duration: i64,
    frames: i64,
}

impl Encoder {
    /// Set `DISCORDO_FORCE_SW_ENCODE=1` to skip the GPU, for testing the fallback.
    pub fn new(gpu: &Gpu, size: (u32, u32), fps: u32, bitrate: u32) -> Result<Self> {
        unsafe {
            if std::env::var_os("DISCORDO_FORCE_SW_ENCODE").is_none() {
                for transform in hardware_encoders() {
                    if let Ok(encoder) = Self::configure(transform, Some(gpu), size, fps, bitrate) {
                        return Ok(encoder);
                    }
                }
            }
            let software: IMFTransform = CoCreateInstance(&CLSID_MSH264EncoderMFT, None, CLSCTX_INPROC_SERVER)?;
            Self::configure(software, None, size, fps, bitrate)
        }
    }

    /// Whether frames are being encoded by the GPU.
    pub fn is_hardware(&self) -> bool {
        self.events.is_some()
    }

    unsafe fn configure(transform: IMFTransform, gpu: Option<&Gpu>, size: (u32, u32), fps: u32, bitrate: u32) -> Result<Self> {
        unsafe {
            let mut events = None;
            if let Some(gpu) = gpu {
                transform.GetAttributes()?.SetUINT32(&MF_TRANSFORM_ASYNC_UNLOCK, 1)?;
                transform.ProcessMessage(MFT_MESSAGE_SET_D3D_MANAGER, gpu.manager.as_raw() as usize)?;
                events = Some(transform.cast::<IMFMediaEventGenerator>()?);
            }

            // Real-time settings, before the types are set. Each is best effort: not every encoder has every knob.
            let codec = transform.cast::<ICodecAPI>().ok();
            if let Some(codec) = &codec {
                let set = |key: &GUID, value: VARIANT| {
                    let _ = codec.SetValue(key, &value);
                };
                set(&CODECAPI_AVEncCommonRateControlMode, VARIANT::from(eAVEncCommonRateControlMode_CBR.0 as u32));
                set(&CODECAPI_AVEncCommonMeanBitRate, VARIANT::from(bitrate));
                set(&CODECAPI_AVLowLatencyMode, VARIANT::from(true)); // no frame reordering, no lookahead
                set(&CODECAPI_AVEncMPVDefaultBPictureCount, VARIANT::from(0u32));
                set(&CODECAPI_AVEncMPVGOPSize, VARIANT::from(fps * 10)); // keyframes mostly on request
            }

            let output = video_type(&MFVideoFormat_H264, size, fps)?;
            output.SetUINT32(&MF_MT_AVG_BITRATE, bitrate)?;
            output.SetUINT32(&MF_MT_MPEG2_PROFILE, eAVEncH264VProfile_Main.0 as u32)?;
            transform.SetOutputType(0, &output, 0)?;
            transform.SetInputType(0, &video_type(&MFVideoFormat_NV12, size, fps)?, 0)?;

            transform.ProcessMessage(MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0)?;
            transform.ProcessMessage(MFT_MESSAGE_NOTIFY_START_OF_STREAM, 0)?;
            Ok(Self { transform, events, codec, wanted: 0, frame_duration: 10_000_000 / fps.max(1) as i64, frames: 0 })
        }
    }

    /// Encodes one NV12 frame. Returns the access units that became ready (usually one; none while
    /// the encoder is still warming up). Each is Annex B and, when a keyframe, starts with SPS/PPS.
    pub fn encode(&mut self, frame: &ID3D11Texture2D, keyframe: bool) -> Result<Vec<Vec<u8>>> {
        unsafe {
            if keyframe && let Some(codec) = &self.codec {
                let _ = codec.SetValue(&CODECAPI_AVEncVideoForceKeyFrame, &VARIANT::from(1u32));
            }
            // The buffer wraps the texture itself. A hardware encoder reads it on the GPU;
            // the software one locks it, which copies the pixels to system memory.
            let buffer = MFCreateDXGISurfaceBuffer(&ID3D11Texture2D::IID, frame, 0, false)?;
            if let Ok(planes) = buffer.cast::<IMF2DBuffer>() {
                buffer.SetCurrentLength(planes.GetContiguousLength()?)?;
            }
            let sample = MFCreateSample()?;
            sample.AddBuffer(&buffer)?;
            sample.SetSampleTime(self.frames * self.frame_duration)?;
            sample.SetSampleDuration(self.frame_duration)?;
            self.frames += 1;

            let mut out = Vec::new();
            if let Some(events) = self.events.clone() {
                while self.wanted == 0 {
                    self.pump(&events, true, &mut out)?;
                }
                self.wanted -= 1;
                self.transform.ProcessInput(0, &sample, 0)?;
                while self.pump(&events, false, &mut out)? {}
            } else {
                self.transform.ProcessInput(0, &sample, 0)?;
                loop {
                    match self.pull() {
                        Ok(unit) => out.push(unit),
                        Err(e) if e.code() == MF_E_TRANSFORM_NEED_MORE_INPUT => break,
                        Err(e) => return Err(e),
                    }
                }
            }
            Ok(out)
        }
    }

    /// Handles one event of an asynchronous encoder. Returns false if none was waiting (non-blocking mode).
    unsafe fn pump(&mut self, events: &IMFMediaEventGenerator, block: bool, out: &mut Vec<Vec<u8>>) -> Result<bool> {
        unsafe {
            let flags = if block { MEDIA_EVENT_GENERATOR_GET_EVENT_FLAGS(0) } else { MF_EVENT_FLAG_NO_WAIT };
            let event = match events.GetEvent(flags) {
                Ok(event) => event,
                Err(e) if e.code() == MF_E_NO_EVENTS_AVAILABLE => return Ok(false),
                Err(e) => return Err(e),
            };
            let kind = event.GetType()?;
            if kind == METransformNeedInput.0 as u32 {
                self.wanted += 1;
            } else if kind == METransformHaveOutput.0 as u32 {
                out.push(self.pull()?);
            }
            Ok(true)
        }
    }

    /// Takes one encoded access unit out of the encoder.
    unsafe fn pull(&mut self) -> Result<Vec<u8>> {
        unsafe {
            let info = self.transform.GetOutputStreamInfo(0)?;
            let provides = (MFT_OUTPUT_STREAM_PROVIDES_SAMPLES.0 | MFT_OUTPUT_STREAM_CAN_PROVIDE_SAMPLES.0) as u32;
            let mut buffers = [MFT_OUTPUT_DATA_BUFFER::default()];
            if info.dwFlags & provides == 0 {
                let sample = MFCreateSample()?;
                sample.AddBuffer(&MFCreateMemoryBuffer(info.cbSize.max(1 << 20))?)?;
                buffers[0].pSample = ManuallyDrop::new(Some(sample));
            }
            let mut status = 0;
            let result = self.transform.ProcessOutput(0, &mut buffers, &mut status);
            let sample = ManuallyDrop::take(&mut buffers[0].pSample);
            ManuallyDrop::drop(&mut buffers[0].pEvents);
            result?;
            let sample = sample.ok_or_else(Error::empty)?;

            let mut data = copy_out(&sample.ConvertToContiguousBuffer()?)?;
            let keyframe = sample.GetUINT32(&MFSampleExtension_CleanPoint).unwrap_or(0) != 0;
            if keyframe && !h264::starts_with_sps(&data) {
                // Some encoders keep SPS/PPS out of band; a viewer joining now needs them in the stream.
                let (mut blob, mut len) = (std::ptr::null_mut(), 0u32);
                if self.transform.GetOutputCurrentType(0)?.GetAllocatedBlob(&MF_MT_MPEG_SEQUENCE_HEADER, &mut blob, &mut len).is_ok() {
                    let mut with_header = std::slice::from_raw_parts(blob, len as usize).to_vec();
                    CoTaskMemFree(Some(blob as _));
                    with_header.append(&mut data);
                    data = with_header;
                }
            }
            Ok(data)
        }
    }
}

impl Drop for Encoder {
    fn drop(&mut self) {
        unsafe {
            let _ = self.transform.ProcessMessage(MFT_MESSAGE_NOTIFY_END_STREAMING, 0);
            if self.events.is_some()
                && let Ok(shutdown) = self.transform.cast::<IMFShutdown>()
            {
                let _ = shutdown.Shutdown(); // hardware encoders hold driver resources until told to let go
            }
        }
    }
}

/// The GPU encoders installed, best first.
unsafe fn hardware_encoders() -> Vec<IMFTransform> {
    unsafe {
        let h264 = MFT_REGISTER_TYPE_INFO { guidMajorType: MFMediaType_Video, guidSubtype: MFVideoFormat_H264 };
        let (mut list, mut count) = (std::ptr::null_mut::<Option<IMFActivate>>(), 0u32);
        let flags = MFT_ENUM_FLAG_HARDWARE | MFT_ENUM_FLAG_SORTANDFILTER;
        if MFTEnumEx(MFT_CATEGORY_VIDEO_ENCODER, flags, None, Some(&h264), &mut list, &mut count).is_err() || list.is_null() {
            return Vec::new();
        }
        let mut found = Vec::new();
        for i in 0..count as usize {
            if let Some(activate) = (*list.add(i)).take()
                && let Ok(transform) = activate.ActivateObject::<IMFTransform>()
            {
                found.push(transform);
            }
        }
        CoTaskMemFree(Some(list as _));
        found
    }
}

pub(super) unsafe fn video_type(subtype: &GUID, size: (u32, u32), fps: u32) -> Result<IMFMediaType> {
    unsafe {
        let pack = |high: u32, low: u32| (high as u64) << 32 | low as u64;
        let t = MFCreateMediaType()?;
        t.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
        t.SetGUID(&MF_MT_SUBTYPE, subtype)?;
        t.SetUINT64(&MF_MT_FRAME_SIZE, pack(size.0, size.1))?;
        t.SetUINT64(&MF_MT_FRAME_RATE, pack(fps, 1))?;
        t.SetUINT64(&MF_MT_PIXEL_ASPECT_RATIO, pack(1, 1))?;
        t.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
        Ok(t)
    }
}

/// Copies a media buffer's bytes out.
pub(super) unsafe fn copy_out(buffer: &IMFMediaBuffer) -> Result<Vec<u8>> {
    unsafe {
        let (mut ptr, mut len) = (std::ptr::null_mut(), 0u32);
        buffer.Lock(&mut ptr, None, Some(&mut len))?;
        let data = std::slice::from_raw_parts(ptr, len as usize).to_vec();
        buffer.Unlock()?;
        Ok(data)
    }
}
