//! A call as the app sees it: the session, our microphone and screen going out, everyone else coming in.

use std::collections::HashMap;
use std::io;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::SyncSender;
use std::sync::{Arc, Mutex};

use crate::codec::Decoder;
use crate::mixer::Mixer;
use crate::session::{Session, SessionEvent};
use crate::transport::{Frame, Kind};

/// What the app needs to hear about. Media itself never leaves this crate.
#[derive(Debug)]
pub enum CallEvent {
    /// Send `{"op": op, "d": {"sdp": sdp}}` over the gateway.
    Signal { op: &'static str, sdp: String },
    Connected,
    /// The media connection was lost; leave and rejoin the call.
    Closed,
    /// A watched video has this picture size now (sent for the first frame and on every change).
    Video { user: String, track: String, width: u32, height: u32 },
    /// Our screen share ended on its own (the window closed, or capture or encoder failure).
    ShareStopped,
}

type Sink = Arc<dyn Fn(CallEvent) + Send + Sync>;

/// A video we are watching: its frames go to the thread that decodes and draws them.
struct Watcher {
    frames: SyncSender<Frame>,
    /// Set when a frame had to be dropped because the decoder fell behind.
    lost: Arc<AtomicBool>,
}

struct Shared {
    mixer: Mutex<Mixer>,
    muted: AtomicBool,
    deafened: AtomicBool,
    /// A viewer needs a keyframe of our screen share.
    keyframe: AtomicBool,
    watchers: Mutex<HashMap<String, Watcher>>,
}

pub struct Call {
    session: Arc<Session>,
    shared: Arc<Shared>,
    #[cfg_attr(not(windows), allow(dead_code))]
    sink: Sink,
    #[cfg(windows)]
    devices: windows_impl::Devices,
}

impl Call {
    /// `sink` is called from background threads and must not block.
    pub fn start(sink: impl Fn(CallEvent) + Send + Sync + 'static) -> io::Result<Self> {
        let sink: Sink = Arc::new(sink);
        let shared = Arc::new(Shared {
            mixer: Mutex::default(),
            muted: AtomicBool::new(false),
            deafened: AtomicBool::new(false),
            keyframe: AtomicBool::new(true),
            watchers: Mutex::default(),
        });

        let (events, incoming) = (sink.clone(), shared.clone());
        let decoders: Mutex<HashMap<String, Decoder>> = Mutex::default();
        let session = Session::start(move |event| match event {
            SessionEvent::Signal { op, sdp } => events(CallEvent::Signal { op, sdp }),
            SessionEvent::Connected { publishing: false } => events(CallEvent::Connected),
            SessionEvent::Connected { publishing: true } => {}
            SessionEvent::Closed => events(CallEvent::Closed),
            SessionEvent::KeyframeRequest { .. } => incoming.keyframe.store(true, Ordering::Relaxed),
            SessionEvent::Frame(frame) => {
                let source = format!("{}/{}", frame.stream, frame.track);
                if frame.kind == Kind::Video {
                    if let Some(watcher) = incoming.watchers.lock().unwrap().get(&source)
                        && watcher.frames.try_send(frame).is_err()
                    {
                        watcher.lost.store(true, Ordering::Relaxed);
                    }
                    return;
                }
                if incoming.deafened.load(Ordering::Relaxed) {
                    return;
                }
                let mut decoders = decoders.lock().unwrap();
                if !decoders.contains_key(&source) {
                    let Some(decoder) = Decoder::new() else { return };
                    decoders.insert(source.clone(), decoder);
                }
                let pcm = decoders.get_mut(&source).unwrap().decode(&frame.data);
                incoming.mixer.lock().unwrap().push(&source, &pcm);
            }
        })?;

        #[cfg(windows)]
        let devices = windows_impl::Devices::open(&shared)?;
        Ok(Self {
            session: Arc::new(session),
            shared,
            sink,
            #[cfg(windows)]
            devices,
        })
    }

    /// Feeds a signaling message from the gateway.
    pub fn signal(&self, typ: &str, sdp: &str) {
        self.session.signal(typ, sdp);
    }

    /// Muted: the microphone stays open but nothing is sent.
    pub fn set_muted(&self, muted: bool) {
        self.shared.muted.store(muted, Ordering::Relaxed);
    }

    /// Deafened: incoming audio is discarded.
    pub fn set_deafened(&self, deafened: bool) {
        self.shared.deafened.store(deafened, Ordering::Relaxed);
    }

    /// Stops showing a video. Tell the server too (`rtc_subscribe` with `on: false`) so it stops sending it.
    pub fn unwatch(&self, user: &str, track: &str) {
        // Dropping the sender ends the player thread.
        self.shared.watchers.lock().unwrap().remove(&format!("{user}/{track}"));
    }
}

#[cfg(windows)]
mod windows_impl {
    use std::ffi::c_void;
    use std::io;
    use std::sync::atomic::{AtomicBool, Ordering};
    use std::sync::{Arc, Mutex, mpsc};
    use std::thread::{self, JoinHandle};
    use std::time::{Duration, Instant};

    use windows::Graphics::Capture::GraphicsCaptureItem;
    use windows::Win32::Graphics::Dxgi::Common::DXGI_FORMAT_NV12;
    use windows::Win32::System::Com::{COINIT_MULTITHREADED, CoInitializeEx};
    use windows::core::Interface;

    use super::{Call, CallEvent, Shared, Watcher};
    use crate::audio_win::{self, Stream};
    use crate::codec::{Encoder as OpusEncoder, FRAME};
    use crate::quality;
    use crate::transport::{Frame, Kind};
    use crate::video_win::capture::ScreenCapture;
    use crate::video_win::encoder::Encoder;
    use crate::video_win::gpu::{Converter, Gpu};
    use crate::video_win::player::Player;

    /// A background thread that runs until told to stop. Dropping it stops and joins the thread.
    struct Worker {
        stop: Arc<AtomicBool>,
        thread: Option<JoinHandle<()>>,
    }

    impl Drop for Worker {
        fn drop(&mut self) {
            self.stop.store(true, Ordering::Relaxed);
            if let Some(thread) = self.thread.take() {
                let _ = thread.join();
            }
        }
    }

    pub struct Devices {
        mic: Mutex<Option<Stream>>,
        share: Mutex<Option<Worker>>,
        _speakers: Stream,
    }

    impl Devices {
        pub fn open(shared: &Arc<Shared>) -> io::Result<Self> {
            let outgoing = shared.clone();
            let speakers = audio_win::playback(move |out| outgoing.mixer.lock().unwrap().mix(out))?;
            Ok(Self { mic: Mutex::default(), share: Mutex::default(), _speakers: speakers })
        }
    }

    impl Call {
        /// Opens the microphone and starts publishing it. Fails if there is no usable microphone.
        pub fn enable_mic(&self) -> io::Result<()> {
            let mut mic = self.devices.mic.lock().unwrap();
            if mic.is_some() {
                return Ok(());
            }
            let mut encoder = OpusEncoder::new().ok_or_else(|| io::Error::other("cannot create the Opus encoder"))?;
            let (session, shared) = (self.session.clone(), self.shared.clone());
            let (mut pending, mut time) = (Vec::with_capacity(FRAME * 2), 0u64);
            // The device delivers whatever chunk sizes it likes; Opus wants exact 20 ms frames.
            *mic = Some(audio_win::capture(move |pcm| {
                pending.extend_from_slice(pcm);
                while pending.len() >= FRAME {
                    if !shared.muted.load(Ordering::Relaxed)
                        && let Some(packet) = encoder.encode(&pending[..FRAME])
                    {
                        session.write("mic", time, packet);
                    }
                    time += FRAME as u64; // time keeps running while muted, so the receiver sees a gap, not a jump
                    pending.drain(..FRAME);
                }
            })?);
            self.session.publish("mic", Kind::Audio);
            Ok(())
        }

        /// Starts (or restarts, with new settings) sharing `item`, a screen or window the user picked,
        /// at up to `height` lines and `fps` frames per second. Returns whether the GPU is doing the encoding.
        pub fn share_screen(&self, item: GraphicsCaptureItem, height: u32, fps: u32) -> io::Result<bool> {
            let mut share = self.devices.share.lock().unwrap();
            *share = None; // stops a share already running
            let fps = fps.clamp(5, 60);
            let stop = Arc::new(AtomicBool::new(false));
            let (session, shared, sink, stopped) = (self.session.clone(), self.shared.clone(), self.sink.clone(), stop.clone());
            let (ready_tx, ready) = mpsc::channel();

            let thread = thread::Builder::new().name("screen-share".into()).spawn(move || {
                let _ = unsafe { CoInitializeEx(None, COINIT_MULTITHREADED) };
                let setup = || -> windows::core::Result<_> {
                    let gpu = Gpu::new()?;
                    let capture = ScreenCapture::new(&gpu, &item)?;
                    let size = quality::output_size(capture.size, height);
                    let converter = Converter::new(&gpu, capture.size, size, fps)?;
                    let nv12 = gpu.texture(size.0, size.1, DXGI_FORMAT_NV12)?;
                    let encoder = Encoder::new(&gpu, size, fps, quality::bitrate(size, fps))?;
                    Ok((gpu, capture, converter, nv12, encoder))
                };
                let (_gpu, capture, converter, nv12, mut encoder) = match setup() {
                    Ok(parts) => {
                        let _ = ready_tx.send(Ok(parts.4.is_hardware()));
                        parts
                    }
                    Err(e) => {
                        let _ = ready_tx.send(Err(e));
                        return;
                    }
                };

                session.publish("screen", Kind::Video);
                shared.keyframe.store(true, Ordering::Relaxed);
                let interval = Duration::from_secs_f64(1.0 / fps as f64);
                let mut run = || -> windows::core::Result<()> {
                    let (mut next, mut have_picture, mut sent) = (Instant::now(), false, 0u64);
                    while !stopped.load(Ordering::Relaxed) && !capture.is_closed() {
                        // A still screen produces no new captures; the last picture is encoded again
                        // (which costs almost nothing) so the stream keeps its frame rate.
                        if let Some((_frame, texture)) = capture.latest() {
                            converter.convert(&texture, 0, &nv12)?;
                            have_picture = true;
                        }
                        if have_picture {
                            let keyframe = shared.keyframe.swap(false, Ordering::Relaxed);
                            for unit in encoder.encode(&nv12, keyframe)? {
                                session.write("screen", sent * 90_000 / fps as u64, unit);
                                sent += 1;
                            }
                        }
                        next += interval;
                        match next.checked_duration_since(Instant::now()) {
                            Some(wait) => thread::sleep(wait),
                            None => next = Instant::now(), // running behind: do not try to catch up
                        }
                    }
                    Ok(())
                };
                let failed = run().is_err() || capture.is_closed();
                session.unpublish("screen");
                if failed {
                    sink(CallEvent::ShareStopped);
                }
            })?;

            let hardware = ready
                .recv()
                .map_err(|_| io::Error::other("screen share thread died while starting"))?
                .map_err(io::Error::other)?;
            *share = Some(Worker { stop, thread: Some(thread) });
            Ok(hardware)
        }

        pub fn stop_share(&self) {
            *self.devices.share.lock().unwrap() = None;
        }

        /// Starts decoding a video track and returns the swap chain it is drawn into, as an
        /// `IDXGISwapChain1*` the caller owns one reference to. Ask the server for the track too
        /// (`rtc_subscribe`). The picture size arrives as [`CallEvent::Video`].
        pub fn watch(&self, user: &str, track: &str) -> io::Result<*mut c_void> {
            let key = format!("{user}/{track}");
            let (frames_tx, frames) = mpsc::sync_channel::<Frame>(8);
            let lost = Arc::new(AtomicBool::new(false));
            let (session, sink, dropped) = (self.session.clone(), self.sink.clone(), lost.clone());
            let (user, track) = (user.to_string(), track.to_string());
            let (ready_tx, ready) = mpsc::channel();

            thread::Builder::new().name("video-player".into()).spawn(move || {
                let _ = unsafe { CoInitializeEx(None, COINIT_MULTITHREADED) };
                let mut player = match Player::new() {
                    Ok(player) => {
                        let _ = ready_tx.send(Ok(player.swap_chain().clone().into_raw() as usize));
                        player
                    }
                    Err(e) => {
                        let _ = ready_tx.send(Err(e));
                        return;
                    }
                };
                session.request_keyframe(&user, &track);
                let mut asked = Instant::now();
                for frame in frames {
                    let contiguous = frame.contiguous && !dropped.swap(false, Ordering::Relaxed);
                    let need_keyframe = match player.push(&frame.data, contiguous) {
                        Ok(pushed) => {
                            if let Some((width, height)) = pushed.resized {
                                sink(CallEvent::Video { user: user.clone(), track: track.clone(), width, height });
                            }
                            pushed.need_keyframe
                        }
                        Err(_) => {
                            dropped.store(true, Ordering::Relaxed); // decoder hiccup: start over at a keyframe
                            true
                        }
                    };
                    if need_keyframe && asked.elapsed() > Duration::from_millis(500) {
                        session.request_keyframe(&user, &track);
                        asked = Instant::now();
                    }
                }
            })?;

            let swap_chain = ready
                .recv()
                .map_err(|_| io::Error::other("video player thread died while starting"))?
                .map_err(io::Error::other)?;
            self.shared.watchers.lock().unwrap().insert(key, Watcher { frames: frames_tx, lost });
            Ok(swap_chain as *mut c_void)
        }
    }
}
