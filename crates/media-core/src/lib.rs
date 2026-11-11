//! Discordo's native media engine.
//!
//! - [`transport`]: one WebRTC connection on its own thread.
//! - [`session`]: a call, i.e. the publish and subscribe connections and their signaling.
//!
//! - [`call`]: a session plus microphone and speakers; what the app uses, through [`ffi`].
//!
//! Everything but the audio devices is platform-neutral, so it is tested off Windows too.

#[cfg(windows)]
mod audio_win;
pub mod call;
pub mod codec;
pub mod ffi;
pub mod mixer;
pub mod session;
pub mod transport;
