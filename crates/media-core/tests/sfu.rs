//! Interop with the real SFU: two sessions in one room, media flowing from one to the other.
//! Needs the Go toolchain; run from the repository with `cargo test`.

use std::collections::HashMap;
use std::io::{BufRead, BufReader, Write};
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::mpsc::{self, Receiver};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use media_core::session::{Session, SessionEvent};
use media_core::transport::{Frame, Kind};
use serde_json::{Value, json};

struct Harness {
    child: Child,
    stdin: Arc<Mutex<ChildStdin>>,
    sessions: Arc<Mutex<HashMap<String, Arc<Session>>>>,
}

impl Harness {
    fn start() -> Self {
        let mut child = Command::new("go")
            .args(["run", "./cmd/sfuharness"])
            .current_dir(concat!(env!("CARGO_MANIFEST_DIR"), "/../../apps/server"))
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .spawn()
            .expect("the Go toolchain is needed to run the SFU");
        let stdin = Arc::new(Mutex::new(child.stdin.take().unwrap()));
        let sessions: Arc<Mutex<HashMap<String, Arc<Session>>>> = Arc::default();

        // Route what the SFU says to the session it is addressed to.
        let stdout = BufReader::new(child.stdout.take().unwrap());
        let routed = sessions.clone();
        thread::spawn(move || {
            for line in stdout.lines() {
                let msg: Value = serde_json::from_str(&line.unwrap()).unwrap();
                let (user, typ) = (msg["user"].as_str().unwrap(), msg["t"].as_str().unwrap());
                assert_ne!(typ, "ERROR", "SFU rejected a message from {user}: {}", msg["d"]);
                let sdp = msg["d"]["sdp"].as_str().unwrap();
                let candidates: Vec<_> = sdp.lines().filter(|l| l.starts_with("a=candidate")).collect();
                eprintln!("sfu -> {user} {typ}: {candidates:?}");
                if let Some(session) = routed.lock().unwrap().get(user) {
                    session.signal(typ, sdp);
                }
            }
        });
        Self { child, stdin, sessions }
    }

    fn send(stdin: &Mutex<ChildStdin>, user: &str, op: &str, d: Value) {
        let line = json!({ "user": user, "op": op, "d": d }).to_string();
        writeln!(stdin.lock().unwrap(), "{line}").unwrap();
    }

    /// Joins the room and returns the session plus the frames it receives.
    fn join(&self, user: &'static str) -> (Arc<Session>, Receiver<Frame>) {
        let (frames_tx, frames) = mpsc::channel();
        let stdin = self.stdin.clone();
        let session = Session::start(move |event| match event {
            SessionEvent::Signal { op, sdp } => {
                // Shown only when the test fails; the candidates are what matters when ICE never connects.
                let candidates: Vec<_> = sdp.lines().filter(|l| l.starts_with("a=candidate")).collect();
                eprintln!("{user} -> {op}: {candidates:?}");
                Self::send(&stdin, user, op, json!({ "sdp": sdp }))
            }
            SessionEvent::Frame(frame) => {
                let _ = frames_tx.send(frame);
            }
            other => eprintln!("{user}: {other:?}"),
        })
        .unwrap();
        let session = Arc::new(session);
        self.sessions.lock().unwrap().insert(user.into(), session.clone());
        Self::send(&self.stdin, user, "join", Value::Null);
        (session, frames)
    }
}

impl Drop for Harness {
    fn drop(&mut self) {
        let _ = self.child.kill();
    }
}

/// Keeps writing `payload` on a track (as an encoder would) until a frame matching `wanted` arrives.
fn pump_until(sender: &Session, track: &str, step: u64, payload: &[u8], frames: &Receiver<Frame>, wanted: impl Fn(&Frame) -> bool) -> Frame {
    let deadline = Instant::now() + Duration::from_secs(90); // the first run also compiles the Go harness
    let mut time = 0;
    while Instant::now() < deadline {
        sender.write(track, time, payload.to_vec());
        time += step;
        if let Ok(frame) = frames.recv_timeout(Duration::from_millis(20))
            && wanted(&frame)
        {
            return frame;
        }
    }
    panic!("no matching frame on {track} within the deadline");
}

#[test]
fn media_flows_through_the_sfu() {
    let harness = Harness::start();
    let (alice, _) = harness.join("alice");
    let (_bob, bob_frames) = harness.join("bob");

    // Audio: forwarded to everyone as soon as it is published.
    let opus = [0xF8, 0xFF, 0xFE, 1, 2, 3, 4, 5]; // not real audio; the transport does not care
    alice.publish("mic", Kind::Audio);
    let frame = pump_until(&alice, "mic", 960, &opus, &bob_frames, |f| f.track == "mic");
    assert_eq!((frame.stream.as_str(), frame.kind), ("alice", Kind::Audio));
    assert_eq!(&frame.data[..], &opus);

    // Video: only after subscribing, and big enough here to be split across several packets.
    let mut h264 = vec![0, 0, 0, 1, 0x67, 0x42, 0xC0, 0x1F, 0, 0, 0, 1, 0x68, 0xCE, 0x3C, 0x80, 0, 0, 0, 1, 0x65];
    h264.extend((0..5000u32).map(|i| (i % 251) as u8 + 1));
    alice.publish("screen", Kind::Video);
    Harness::send(&harness.stdin, "bob", "rtc_subscribe", json!({ "user_id": "alice", "track": "screen", "on": true }));
    let frame = pump_until(&alice, "screen", 3000, &h264, &bob_frames, |f| f.track == "screen");
    assert_eq!((frame.stream.as_str(), frame.kind), ("alice", Kind::Video));
    let idr = &h264[h264.len() - 5000..];
    assert!(frame.data.windows(idr.len()).any(|w| w == idr), "the IDR slice did not arrive intact ({} bytes)", frame.data.len());
}
