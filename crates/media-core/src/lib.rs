//! Discordo's native media engine.
//!
//! - [`transport`]: one WebRTC connection on its own thread.
//! - [`session`]: a call, i.e. the publish and subscribe connections and their signaling.
//!
//! Both are platform-neutral. Capture, encoding and playback are Windows-only and sit on top.

pub mod session;
pub mod transport;
