//! Stream quality presets: what resolution to encode at and how many bits to spend.

/// Heights the user can pick, 480p to 4K.
pub const HEIGHTS: [u32; 5] = [480, 720, 1080, 1440, 2160];

/// The encoded size for a source and a requested height: the source's aspect ratio is kept,
/// nothing is ever upscaled, and both dimensions are even (H.264 with 4:2:0 chroma needs that).
pub fn output_size(source: (u32, u32), height: u32) -> (u32, u32) {
    let (w, h) = source;
    let out_h = height.min(h).max(2);
    let out_w = (w as u64 * out_h as u64 / h.max(1) as u64) as u32;
    (out_w.max(2) & !1, out_h & !1)
}

/// Target bitrate in bits per second for screen content.
///
/// Scales with the pixel rate, less than linearly (bigger frames compress better): about
/// 1.3 Mbit/s at 480p30, 5 at 1080p30, 9 at 1080p60, 16 at 4K30 and 29 at 4K60.
pub fn bitrate(size: (u32, u32), fps: u32) -> u32 {
    let pixel_rate = size.0 as f64 * size.1 as f64 * fps as f64;
    let reference = 1920.0 * 1080.0 * 30.0;
    (5_000_000.0 * (pixel_rate / reference).powf(0.85)) as u32
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sizes_keep_aspect_and_never_upscale() {
        assert_eq!(output_size((3840, 2160), 1080), (1920, 1080));
        assert_eq!(output_size((1920, 1080), 2160), (1920, 1080), "no upscaling");
        assert_eq!(output_size((3440, 1440), 720), (1720, 720), "ultrawide stays ultrawide");
        assert_eq!(output_size((1366, 768), 480), (852, 480), "odd widths round down to even");
    }

    #[test]
    fn bitrates_match_the_presets() {
        let mbps = |w, h, fps| bitrate((w, h), fps) as f64 / 1e6;
        assert!((1.2..1.8).contains(&mbps(854, 480, 30)));
        assert!((4.9..5.1).contains(&mbps(1920, 1080, 30)));
        assert!((8.5..10.0).contains(&mbps(1920, 1080, 60)));
        assert!((16.0..19.0).contains(&mbps(3840, 2160, 30)));
        assert!((28.0..34.0).contains(&mbps(3840, 2160, 60)));
    }
}
