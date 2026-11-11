//! Video on Windows, on the GPU from end to end:
//!
//! sending:   screen capture (BGRA texture) -> [`gpu::Converter`] (scale + NV12) -> [`encoder`] (H.264)
//! receiving: H.264 -> [`player`] (decode to NV12 texture -> BGRA swap chain shown by the app)
//!
//! Frames stay in GPU memory throughout; only compressed bytes cross to the CPU.

pub mod capture;
pub mod encoder;
pub mod gpu;
pub mod player;
