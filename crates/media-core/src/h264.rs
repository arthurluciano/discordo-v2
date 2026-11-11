//! The little H.264 bitstream knowledge the pipelines need (Annex B: NAL units separated by start codes).

const IDR: u8 = 5;
const SPS: u8 = 7;

/// The type of every NAL unit in an access unit, in order.
fn nal_types(data: &[u8]) -> impl Iterator<Item = u8> + '_ {
    // A start code is 00 00 01 (possibly preceded by more zeros); the NAL header follows it.
    data.windows(4).filter(|w| w[..3] == [0, 0, 1]).map(|w| w[3] & 0x1F)
}

/// Whether decoding can start at this access unit.
pub fn is_keyframe(data: &[u8]) -> bool {
    nal_types(data).any(|t| t == IDR || t == SPS)
}

/// Whether the access unit carries its own parameter sets, as a decoder joining mid-stream needs.
pub fn starts_with_sps(data: &[u8]) -> bool {
    nal_types(data).next() == Some(SPS)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn recognizes_frame_kinds() {
        let key = [0, 0, 0, 1, 0x67, 1, 2, 0, 0, 0, 1, 0x68, 3, 0, 0, 1, 0x65, 9, 9];
        let idr_only = [0, 0, 1, 0x65, 9, 9];
        let delta = [0, 0, 0, 1, 0x41, 9, 9, 9];
        assert!(is_keyframe(&key) && starts_with_sps(&key));
        assert!(is_keyframe(&idr_only) && !starts_with_sps(&idr_only));
        assert!(!is_keyframe(&delta) && !starts_with_sps(&delta));
        assert!(!is_keyframe(&[]));
    }
}
