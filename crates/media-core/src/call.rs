//! A voice call as the app sees it: the session, the microphone going out and everyone else coming in.

use std::collections::HashMap;
use std::io;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use crate::codec::{Decoder, Encoder, FRAME};
use crate::mixer::Mixer;
use crate::session::{Session, SessionEvent};
use crate::transport::Kind;

/// What the app needs to hear about. Media itself never leaves this crate.
#[derive(Debug)]
pub enum CallEvent {
    /// Send `{"op": op, "d": {"sdp": sdp}}` over the gateway.
    Signal { op: &'static str, sdp: String },
    Connected,
    /// The media connection was lost; leave and rejoin the call.
    Closed,
}

struct Shared {
    mixer: Mutex<Mixer>,
    muted: AtomicBool,
    deafened: AtomicBool,
}

pub struct Call {
    session: Arc<Session>,
    shared: Arc<Shared>,
    #[cfg(windows)]
    mic: Mutex<Option<crate::audio_win::Stream>>,
    #[cfg(windows)]
    _speakers: crate::audio_win::Stream,
}

impl Call {
    /// `sink` is called from background threads and must not block.
    pub fn start(sink: impl Fn(CallEvent) + Send + Sync + 'static) -> io::Result<Self> {
        let shared = Arc::new(Shared { mixer: Mutex::default(), muted: AtomicBool::new(false), deafened: AtomicBool::new(false) });

        let incoming = shared.clone();
        let decoders: Mutex<HashMap<String, Decoder>> = Mutex::default();
        let session = Session::start(move |event| match event {
            SessionEvent::Signal { op, sdp } => sink(CallEvent::Signal { op, sdp }),
            SessionEvent::Connected { publishing: false } => sink(CallEvent::Connected),
            SessionEvent::Closed => sink(CallEvent::Closed),
            SessionEvent::Frame(frame) if frame.kind == Kind::Audio => {
                if incoming.deafened.load(Ordering::Relaxed) {
                    return;
                }
                let source = format!("{}/{}", frame.stream, frame.track);
                let mut decoders = decoders.lock().unwrap();
                if !decoders.contains_key(&source) {
                    let Some(decoder) = Decoder::new() else { return };
                    decoders.insert(source.clone(), decoder);
                }
                let pcm = decoders.get_mut(&source).unwrap().decode(&frame.data);
                incoming.mixer.lock().unwrap().push(&source, &pcm);
            }
            // Video frames and keyframe requests: the video pipeline is not built yet.
            _ => {}
        })?;

        #[cfg(windows)]
        let speakers = {
            let outgoing = shared.clone();
            crate::audio_win::playback(move |out| outgoing.mixer.lock().unwrap().mix(out))?
        };

        Ok(Self {
            session: Arc::new(session),
            shared,
            #[cfg(windows)]
            mic: Mutex::default(),
            #[cfg(windows)]
            _speakers: speakers,
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

    /// Opens the microphone and starts publishing it. Fails if there is no usable microphone.
    #[cfg(windows)]
    pub fn enable_mic(&self) -> io::Result<()> {
        let mut mic = self.mic.lock().unwrap();
        if mic.is_some() {
            return Ok(());
        }
        let mut encoder = Encoder::new().ok_or_else(|| io::Error::other("cannot create the Opus encoder"))?;
        let (session, shared) = (self.session.clone(), self.shared.clone());
        let (mut pending, mut time) = (Vec::with_capacity(FRAME * 2), 0u64);
        // The device delivers whatever chunk sizes it likes; Opus wants exact 20 ms frames.
        *mic = Some(crate::audio_win::capture(move |pcm| {
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
}

// Off Windows there are no devices; this keeps the unused-code lints honest about what is Windows-only.
#[cfg(not(windows))]
const _: fn() = || {
    let _ = (Encoder::new, FRAME);
};
