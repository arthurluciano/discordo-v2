//! Microphone and speakers through WASAPI (shared mode, event-driven, 48 kHz 16-bit).
//!
//! Streams are opened in the *Communications* category, which is what makes Windows apply its
//! echo cancellation and noise suppression to the microphone on hardware that supports it. We opt
//! out of the ducking that comes with it, so other apps' sound keeps its volume during a call.
//! shortcut: relies on those system effects; bring our own (webrtc-audio-processing) if echo is a problem.

use std::io;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc;
use std::thread::{self, JoinHandle};

use windows::Win32::Foundation::{CloseHandle, HANDLE, WAIT_OBJECT_0};
use windows::Win32::Media::Audio::{
    AUDCLNT_BUFFERFLAGS_SILENT, AUDCLNT_SHAREMODE_SHARED, AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM,
    AUDCLNT_STREAMFLAGS_EVENTCALLBACK, AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY, AUDCLNT_STREAMOPTIONS_NONE,
    AudioCategory_Communications, AudioClientProperties, EDataFlow, IAudioCaptureClient, IAudioClient,
    IAudioClient2, IAudioRenderClient, IAudioSessionControl, IAudioSessionControl2, IMMDeviceEnumerator,
    MMDeviceEnumerator, WAVE_FORMAT_PCM, WAVEFORMATEX, eCapture, eConsole, eRender,
};
use windows::Win32::System::Com::{CLSCTX_ALL, COINIT_MULTITHREADED, CoCreateInstance, CoInitializeEx, CoUninitialize};
use windows::Win32::System::Threading::{CreateEventW, WaitForSingleObject};
use windows::core::{Interface, Result};

use crate::codec::RATE;

/// A running capture or playback stream. Dropping it stops the stream.
pub struct Stream {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<()>>,
}

impl Drop for Stream {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
    }
}

/// Captures the default microphone. `on_pcm` gets mono samples as they arrive.
pub fn capture(mut on_pcm: impl FnMut(&[i16]) + Send + 'static) -> io::Result<Stream> {
    spawn("audio-capture", move |stop| unsafe {
        let (client, event) = open(eCapture, 1)?;
        let capture: IAudioCaptureClient = client.GetService()?;
        Ok(Box::new(move || {
            let mut run = || -> Result<()> {
                client.Start()?;
                while !stop.load(Ordering::Relaxed) {
                    if WaitForSingleObject(event, 200) != WAIT_OBJECT_0 {
                        continue;
                    }
                    while capture.GetNextPacketSize()? > 0 {
                        let (mut data, mut frames, mut flags) = (std::ptr::null_mut(), 0u32, 0u32);
                        capture.GetBuffer(&mut data, &mut frames, &mut flags, None, None)?;
                        if flags & AUDCLNT_BUFFERFLAGS_SILENT.0 as u32 != 0 {
                            on_pcm(&vec![0; frames as usize]);
                        } else {
                            on_pcm(std::slice::from_raw_parts(data.cast::<i16>(), frames as usize));
                        }
                        capture.ReleaseBuffer(frames)?;
                    }
                }
                client.Stop()
            };
            let _ = run(); // the device went away (unplugged): the stream just ends
            let _ = CloseHandle(event);
        }))
    })
}

/// Plays on the default output device. `fill` must fill the buffer with mono samples.
pub fn playback(mut fill: impl FnMut(&mut [i16]) + Send + 'static) -> io::Result<Stream> {
    spawn("audio-playback", move |stop| unsafe {
        let (client, event) = open(eRender, 2)?;
        let render: IAudioRenderClient = client.GetService()?;
        let capacity = client.GetBufferSize()?;
        Ok(Box::new(move || {
            let mut mono = Vec::new();
            let mut run = || -> Result<()> {
                client.Start()?;
                while !stop.load(Ordering::Relaxed) {
                    if WaitForSingleObject(event, 200) != WAIT_OBJECT_0 {
                        continue;
                    }
                    let frames = capacity - client.GetCurrentPadding()?;
                    if frames == 0 {
                        continue;
                    }
                    mono.resize(frames as usize, 0);
                    fill(&mut mono);
                    let data = render.GetBuffer(frames)?;
                    let stereo = std::slice::from_raw_parts_mut(data.cast::<i16>(), frames as usize * 2);
                    for (i, sample) in mono.iter().enumerate() {
                        stereo[i * 2] = *sample;
                        stereo[i * 2 + 1] = *sample;
                    }
                    render.ReleaseBuffer(frames, 0)?;
                }
                client.Stop()
            };
            let _ = run();
            let _ = CloseHandle(event);
        }))
    })
}

type Body = Box<dyn FnOnce()>;

/// Runs a stream on its own thread. `setup` opens the device there (COM objects stay on the thread
/// that made them) and returns the loop to run; a setup error is reported to the caller.
fn spawn(name: &str, setup: impl FnOnce(Arc<AtomicBool>) -> Result<Body> + Send + 'static) -> io::Result<Stream> {
    let stop = Arc::new(AtomicBool::new(false));
    let thread_stop = stop.clone();
    let (ready_tx, ready) = mpsc::channel();
    let thread = thread::Builder::new().name(name.into()).spawn(move || unsafe {
        let com = CoInitializeEx(None, COINIT_MULTITHREADED);
        match setup(thread_stop) {
            Ok(body) => {
                let _ = ready_tx.send(Ok(()));
                body();
            }
            Err(e) => {
                let _ = ready_tx.send(Err(e));
            }
        }
        if com.is_ok() {
            CoUninitialize();
        }
    })?;
    match ready.recv() {
        Ok(Ok(())) => Ok(Stream { stop, thread: Some(thread) }),
        Ok(Err(e)) => Err(io::Error::other(e)),
        Err(_) => Err(io::Error::other("audio thread died while starting")),
    }
}

/// Opens the default device for `flow` with our fixed format; Windows converts to and from the device's own.
/// The default device, not the "communications" one: that is what people expect to be used.
unsafe fn open(flow: EDataFlow, channels: u16) -> Result<(IAudioClient, HANDLE)> {
    unsafe {
        let enumerator: IMMDeviceEnumerator = CoCreateInstance(&MMDeviceEnumerator, None, CLSCTX_ALL)?;
        let device = enumerator.GetDefaultAudioEndpoint(flow, eConsole)?;
        let client: IAudioClient = device.Activate(CLSCTX_ALL, None)?;
        if let Ok(client2) = client.cast::<IAudioClient2>() {
            let properties = AudioClientProperties {
                cbSize: size_of::<AudioClientProperties>() as u32,
                bIsOffload: false.into(),
                eCategory: AudioCategory_Communications,
                Options: AUDCLNT_STREAMOPTIONS_NONE,
            };
            let _ = client2.SetClientProperties(&properties); // best effort: without it we only lose the voice effects
        }
        let block = channels * 2;
        let format = WAVEFORMATEX {
            wFormatTag: WAVE_FORMAT_PCM as u16,
            nChannels: channels,
            nSamplesPerSec: RATE,
            nAvgBytesPerSec: RATE * block as u32,
            nBlockAlign: block,
            wBitsPerSample: 16,
            cbSize: 0,
        };
        let flags = AUDCLNT_STREAMFLAGS_EVENTCALLBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY;
        const BUFFER_100NS: i64 = 200_000; // 20 ms
        client.Initialize(AUDCLNT_SHAREMODE_SHARED, flags, BUFFER_100NS, 0, &format, None)?;
        // Without this Windows lowers every other app's volume while we are in a call.
        if let Ok(control) = client.GetService::<IAudioSessionControl>()
            && let Ok(control) = control.cast::<IAudioSessionControl2>()
        {
            let _ = control.SetDuckingPreference(true);
        }
        let event = CreateEventW(None, false, false, None)?;
        client.SetEventHandle(event)?;
        Ok((client, event))
    }
}
