//! One WebRTC connection, driven on its own thread.
//!
//! str0m is sans-IO: this file is the IO. The thread owns the UDP socket and the `Rtc` state
//! machine; everything else talks to it through [`Transport`], which queues a command and wakes
//! the thread with a datagram on loopback.

use std::collections::HashMap;
use std::io::{self, ErrorKind};
use std::net::{IpAddr, Ipv4Addr, SocketAddr, UdpSocket};
use std::sync::mpsc::{self, Receiver, Sender, TryRecvError};
use std::sync::{Arc, Once};
use std::thread;
use std::time::{Duration, Instant};

use str0m::change::{SdpAnswer, SdpOffer, SdpPendingOffer};
use str0m::format::Codec;
use str0m::media::{Direction, KeyframeRequestKind, MediaKind, MediaTime, Mid};
use str0m::net::{Protocol, Receive};
use str0m::{Candidate, Event, IceConnectionState, Input, Output, Rtc, RtcConfig};

/// What a track carries; decides the codec (Opus or H.264).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Audio,
    Video,
}

/// One encoded frame received from the other side.
#[derive(Clone, Debug)]
pub struct Frame {
    /// Stream ID of the track: the SFU sets it to the sender's user ID.
    pub stream: String,
    /// Track ID: "mic", "camera", "screen", "screen-audio".
    pub track: String,
    pub kind: Kind,
    /// Presentation time in the codec's clock (48 kHz audio, 90 kHz video).
    pub time: u64,
    /// False when packets were lost before this frame; a video decoder must then wait for a keyframe.
    pub contiguous: bool,
    pub data: Arc<[u8]>,
}

#[derive(Debug)]
pub enum Out {
    /// Send this SDP to the other side.
    Offer(String),
    Answer(String),
    Connected,
    /// The connection is gone for good.
    Closed,
    Frame(Frame),
    /// The receiver of one of our video tracks needs a keyframe.
    KeyframeRequest { track: String },
}

enum Cmd {
    Publish { track: String, kind: Kind },
    Unpublish { track: String },
    RemoteOffer(String),
    RemoteAnswer(String),
    Write { track: String, time: u64, data: Arc<[u8]> },
    RequestKeyframe { stream: String, track: String },
    Close,
}

/// Handle to a running connection. Dropping it closes the connection.
pub struct Transport {
    tx: Sender<Cmd>,
    waker: UdpSocket,
    wake_addr: SocketAddr,
}

impl Transport {
    /// Starts the connection thread. `sink` is called on that thread and must not block.
    pub fn start(sink: impl Fn(Out) + Send + 'static) -> io::Result<Self> {
        static CRYPTO: Once = Once::new();
        CRYPTO.call_once(|| str0m::crypto::from_feature_flags().install_process_default());

        let socket = UdpSocket::bind((Ipv4Addr::UNSPECIFIED, 0))?;
        let port = socket.local_addr()?.port();
        let local = SocketAddr::new(local_ip()?, port);
        let waker = UdpSocket::bind((Ipv4Addr::LOCALHOST, 0))?;
        let waker_addr = waker.local_addr()?;

        let mut rtc = RtcConfig::new()
            .clear_codecs()
            .enable_opus(true, false)
            .enable_h264(true)
            .build(Instant::now());
        let candidate = Candidate::host(local, "udp").map_err(io::Error::other)?;
        rtc.add_local_candidate(candidate);

        let (tx, rx) = mpsc::channel();
        let mut conn = Conn {
            rtc,
            sink: Box::new(sink),
            pending: None,
            queued: Vec::new(),
            sending: HashMap::new(),
            receiving: HashMap::new(),
        };
        thread::Builder::new()
            .name("media-transport".into())
            .spawn(move || conn.run(socket, local, waker_addr, rx))?;

        Ok(Self { tx, waker, wake_addr: SocketAddr::new(IpAddr::V4(Ipv4Addr::LOCALHOST), port) })
    }

    /// Starts sending a track. Triggers an [`Out::Offer`].
    pub fn publish(&self, track: &str, kind: Kind) {
        self.send(Cmd::Publish { track: track.into(), kind });
    }

    pub fn unpublish(&self, track: &str) {
        self.send(Cmd::Unpublish { track: track.into() });
    }

    /// The other side's offer; answered with an [`Out::Answer`].
    pub fn remote_offer(&self, sdp: &str) {
        self.send(Cmd::RemoteOffer(sdp.into()));
    }

    /// The other side's answer to our last [`Out::Offer`].
    pub fn remote_answer(&self, sdp: &str) {
        self.send(Cmd::RemoteAnswer(sdp.into()));
    }

    /// Sends one encoded frame (an Opus packet, or an H.264 access unit in Annex B) on a published track.
    /// `time` is in the codec's clock. Frames written before the connection is up are dropped.
    pub fn write(&self, track: &str, time: u64, data: impl Into<Arc<[u8]>>) {
        self.send(Cmd::Write { track: track.into(), time, data: data.into() });
    }

    /// Asks the sender of a received video track for a keyframe.
    pub fn request_keyframe(&self, stream: &str, track: &str) {
        self.send(Cmd::RequestKeyframe { stream: stream.into(), track: track.into() });
    }

    fn send(&self, cmd: Cmd) {
        // Both fail only when the thread is gone, and then there is nobody to tell.
        let _ = self.tx.send(cmd);
        let _ = self.waker.send_to(&[0], self.wake_addr);
    }
}

impl Drop for Transport {
    fn drop(&mut self) {
        self.send(Cmd::Close);
    }
}

/// The address other hosts reach us at: the source address of the default route.
/// Connecting a UDP socket sends nothing; it only makes the OS pick the route.
fn local_ip() -> io::Result<IpAddr> {
    let probe = UdpSocket::bind((Ipv4Addr::UNSPECIFIED, 0))?;
    match probe.connect((Ipv4Addr::new(192, 0, 2, 1), 9)) {
        Ok(()) => Ok(probe.local_addr()?.ip()),
        Err(_) => Ok(IpAddr::V4(Ipv4Addr::LOCALHOST)), // no network at all: loopback still works for local testing
    }
}

enum Change {
    Add { track: String, kind: Kind },
    Remove { track: String },
}

/// The connection state, owned by its thread.
struct Conn {
    rtc: Rtc,
    sink: Box<dyn Fn(Out) + Send>,
    /// Our offer that the other side has not answered yet.
    pending: Option<SdpPendingOffer>,
    /// Changes made while an offer was pending; they go into the next one.
    queued: Vec<Change>,
    sending: HashMap<String, (Mid, Kind)>,
    receiving: HashMap<Mid, (String, String, Kind)>,
}

impl Conn {
    fn run(&mut self, socket: UdpSocket, local: SocketAddr, waker: SocketAddr, rx: Receiver<Cmd>) {
        let mut buf = vec![0u8; 2000];
        loop {
            loop {
                match rx.try_recv() {
                    Ok(Cmd::Close) | Err(TryRecvError::Disconnected) => {
                        self.rtc.disconnect();
                        return;
                    }
                    Ok(cmd) => self.handle(cmd),
                    Err(TryRecvError::Empty) => break,
                }
            }

            let deadline = loop {
                match self.rtc.poll_output() {
                    Ok(Output::Timeout(at)) => break at,
                    Ok(Output::Transmit(t)) => {
                        let _ = socket.send_to(&t.contents, t.destination); // a lost packet is the network's business
                    }
                    Ok(Output::Event(event)) => self.on_event(event),
                    Err(_) => {
                        (self.sink)(Out::Closed);
                        return;
                    }
                }
            };
            if !self.rtc.is_alive() {
                (self.sink)(Out::Closed);
                return;
            }

            // Zero is rejected by set_read_timeout, hence the floor.
            let wait = deadline.saturating_duration_since(Instant::now()).max(Duration::from_millis(1));
            let _ = socket.set_read_timeout(Some(wait));
            let input = match socket.recv_from(&mut buf) {
                Ok((_, source)) if source == waker => continue,
                Ok((n, source)) => match buf[..n].try_into() {
                    Ok(contents) => Input::Receive(
                        Instant::now(),
                        Receive { proto: Protocol::Udp, source, destination: local, contents },
                    ),
                    Err(_) => continue, // not STUN, DTLS or RTP
                },
                Err(e) if matches!(e.kind(), ErrorKind::WouldBlock | ErrorKind::TimedOut) => {
                    Input::Timeout(Instant::now())
                }
                // Windows reports ICMP "port unreachable" from an earlier send as an error here; not fatal.
                Err(_) => continue,
            };
            if self.rtc.handle_input(input).is_err() {
                (self.sink)(Out::Closed);
                return;
            }
        }
    }

    fn handle(&mut self, cmd: Cmd) {
        match cmd {
            Cmd::Publish { track, kind } => {
                self.queued.push(Change::Add { track, kind });
                self.negotiate();
            }
            Cmd::Unpublish { track } => {
                self.queued.push(Change::Remove { track });
                self.negotiate();
            }
            Cmd::RemoteAnswer(sdp) => {
                if let (Some(pending), Ok(answer)) = (self.pending.take(), SdpAnswer::from_sdp_string(&sdp))
                    && self.rtc.sdp_api().accept_answer(pending, answer).is_err()
                {
                    self.rtc.disconnect(); // run() reports Closed
                }
                self.negotiate();
            }
            Cmd::RemoteOffer(sdp) => {
                let Ok(offer) = SdpOffer::from_sdp_string(&sdp) else { return };
                if let Ok(answer) = self.rtc.sdp_api().accept_offer(offer) {
                    (self.sink)(Out::Answer(answer.to_sdp_string()));
                }
            }
            Cmd::Write { track, time, data } => {
                let Some(&(mid, kind)) = self.sending.get(&track) else { return };
                let Some(writer) = self.rtc.writer(mid) else { return };
                let (codec, clock) = match kind {
                    Kind::Audio => (Codec::Opus, str0m::media::Frequency::FORTY_EIGHT_KHZ),
                    Kind::Video => (Codec::H264, str0m::media::Frequency::NINETY_KHZ),
                };
                let Some(pt) = writer.payload_params().find(|p| p.spec().codec == codec).map(|p| p.pt()) else {
                    return; // not negotiated yet
                };
                let _ = writer.write(pt, Instant::now(), MediaTime::new(time, clock), data);
            }
            Cmd::RequestKeyframe { stream, track } => {
                let mid = self.receiving.iter().find(|(_, (s, t, _))| *s == stream && *t == track).map(|(mid, _)| *mid);
                if let Some(mut writer) = mid.and_then(|mid| self.rtc.writer(mid)) {
                    let _ = writer.request_keyframe(None, KeyframeRequestKind::Pli);
                }
            }
            Cmd::Close => unreachable!("handled by run"),
        }
    }

    /// Offers the queued changes, unless an offer is already waiting for its answer.
    fn negotiate(&mut self) {
        if self.pending.is_some() || self.queued.is_empty() {
            return;
        }
        let mut api = self.rtc.sdp_api();
        for change in self.queued.drain(..) {
            match change {
                Change::Add { track, kind } => {
                    if self.sending.contains_key(&track) {
                        continue;
                    }
                    let media = match kind {
                        Kind::Audio => MediaKind::Audio,
                        Kind::Video => MediaKind::Video,
                    };
                    let mid = api.add_media(media, Direction::SendOnly, Some("discordo".into()), Some(track.clone()), None);
                    self.sending.insert(track, (mid, kind));
                }
                Change::Remove { track } => {
                    if let Some((mid, _)) = self.sending.remove(&track) {
                        api.stop_media(mid);
                    }
                }
            }
        }
        if let Some((offer, pending)) = api.apply() {
            self.pending = Some(pending);
            (self.sink)(Out::Offer(offer.to_sdp_string()));
        }
    }

    fn on_event(&mut self, event: Event) {
        match event {
            Event::IceConnectionStateChange(IceConnectionState::Connected | IceConnectionState::Completed) => {
                (self.sink)(Out::Connected);
            }
            Event::IceConnectionStateChange(IceConnectionState::Disconnected) => {
                self.rtc.disconnect(); // run() reports Closed once the state machine winds down
            }
            Event::MediaAdded(added) => {
                if let Some(media) = self.rtc.media(added.mid) {
                    let kind = if added.kind == MediaKind::Audio { Kind::Audio } else { Kind::Video };
                    self.receiving.insert(added.mid, (media.stream_id().to_string(), media.track_id().to_string(), kind));
                }
            }
            Event::MediaData(data) => {
                let Some((stream, track, kind)) = self.receiving.get(&data.mid) else { return };
                (self.sink)(Out::Frame(Frame {
                    stream: stream.clone(),
                    track: track.clone(),
                    kind: *kind,
                    time: data.time.numer(),
                    contiguous: data.contiguous,
                    data: data.data,
                }));
            }
            Event::KeyframeRequest(request) => {
                if let Some((track, _)) = self.sending.iter().find(|(_, (mid, _))| *mid == request.mid) {
                    (self.sink)(Out::KeyframeRequest { track: track.clone() });
                }
            }
            _ => {}
        }
    }
}
