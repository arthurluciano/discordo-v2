//! A call: two connections to the SFU and the signaling that keeps them in sync.
//!
//! The publish connection carries what we send (we offer, the SFU answers). The subscribe
//! connection carries what we receive (the SFU offers, we answer). Signaling messages travel
//! over the app's gateway WebSocket, which this crate does not own: outgoing ones are handed
//! to the sink as [`SessionEvent::Signal`], incoming ones are fed to [`Session::signal`].

use std::io;
use std::sync::Arc;

use crate::transport::{Frame, Kind, Out, Transport};

#[derive(Debug)]
pub enum SessionEvent {
    /// Send `{"op": op, "d": {"sdp": sdp}}` over the gateway.
    Signal { op: &'static str, sdp: String },
    /// Media can flow in the given direction.
    Connected { publishing: bool },
    /// A connection was lost; the call should be rejoined.
    Closed,
    Frame(Frame),
    /// Encode a keyframe now on this published video track.
    KeyframeRequest { track: String },
}

pub struct Session {
    publish: Transport,
    subscribe: Transport,
}

impl Session {
    /// `sink` is called from the connection threads and must not block.
    pub fn start(sink: impl Fn(SessionEvent) + Send + Sync + 'static) -> io::Result<Self> {
        let sink = Arc::new(sink);
        let pub_sink = sink.clone();
        let publish = Transport::start(move |out| match out {
            Out::Offer(sdp) => pub_sink(SessionEvent::Signal { op: "rtc_offer", sdp }),
            Out::Connected => pub_sink(SessionEvent::Connected { publishing: true }),
            Out::Closed => pub_sink(SessionEvent::Closed),
            Out::KeyframeRequest { track } => pub_sink(SessionEvent::KeyframeRequest { track }),
            Out::Answer(_) | Out::Frame(_) => {} // this connection never receives offers or media
        })?;
        let subscribe = Transport::start(move |out| match out {
            Out::Answer(sdp) => sink(SessionEvent::Signal { op: "rtc_answer", sdp }),
            Out::Connected => sink(SessionEvent::Connected { publishing: false }),
            Out::Closed => sink(SessionEvent::Closed),
            Out::Frame(frame) => sink(SessionEvent::Frame(frame)),
            Out::Offer(_) | Out::KeyframeRequest { .. } => {}
        })?;
        Ok(Self { publish, subscribe })
    }

    /// Feeds a signaling message from the gateway (`RTC_ANSWER` or `RTC_OFFER`). Others are ignored.
    pub fn signal(&self, typ: &str, sdp: &str) {
        match typ {
            "RTC_ANSWER" => self.publish.remote_answer(sdp),
            "RTC_OFFER" => self.subscribe.remote_offer(sdp),
            _ => {}
        }
    }

    pub fn publish(&self, track: &str, kind: Kind) {
        self.publish.publish(track, kind);
    }

    pub fn unpublish(&self, track: &str) {
        self.publish.unpublish(track);
    }

    /// See [`Transport::write`].
    pub fn write(&self, track: &str, time: u64, data: impl Into<Arc<[u8]>>) {
        self.publish.write(track, time, data);
    }

    /// Asks `user` for a keyframe on a video track we receive from them.
    pub fn request_keyframe(&self, user: &str, track: &str) {
        self.subscribe.request_keyframe(user, track);
    }
}
