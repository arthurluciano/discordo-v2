//! Opus, the voice codec. 48 kHz mono, 20 ms frames.

/// Samples per second, and per frame.
pub const RATE: u32 = 48_000;
pub const FRAME: usize = 960;

#[cfg(feature = "opus")]
mod imp {
    use super::{FRAME, RATE};
    use opus::{Application, Bitrate, Channels};

    pub struct Encoder(opus::Encoder);

    impl Encoder {
        pub fn new() -> Option<Self> {
            let mut enc = opus::Encoder::new(RATE, Channels::Mono, Application::Voip).ok()?;
            // 48 kbit/s is transparent for speech; FEC lets the receiver rebuild a lost packet from the next one.
            enc.set_bitrate(Bitrate::Bits(48_000)).ok()?;
            enc.set_inband_fec(true).ok()?;
            enc.set_packet_loss_perc(10).ok()?;
            Some(Self(enc))
        }

        /// Encodes exactly one frame (`FRAME` samples).
        pub fn encode(&mut self, pcm: &[i16]) -> Option<Vec<u8>> {
            let mut out = vec![0u8; 1275]; // the largest Opus packet
            let n = self.0.encode(pcm, &mut out).ok()?;
            out.truncate(n);
            Some(out)
        }
    }

    pub struct Decoder(opus::Decoder);

    impl Decoder {
        pub fn new() -> Option<Self> {
            opus::Decoder::new(RATE, Channels::Mono).ok().map(Self)
        }

        /// Decodes one packet; a corrupt one yields nothing.
        pub fn decode(&mut self, packet: &[u8]) -> Vec<i16> {
            let mut pcm = vec![0i16; FRAME * 6]; // a packet holds at most 120 ms
            let n = self.0.decode(packet, &mut pcm, false).unwrap_or(0);
            pcm.truncate(n);
            pcm
        }
    }
}

#[cfg(not(feature = "opus"))]
mod imp {
    pub struct Encoder;

    impl Encoder {
        pub fn new() -> Option<Self> {
            Some(Self)
        }

        pub fn encode(&mut self, _pcm: &[i16]) -> Option<Vec<u8>> {
            None
        }
    }

    pub struct Decoder;

    impl Decoder {
        pub fn new() -> Option<Self> {
            Some(Self)
        }

        pub fn decode(&mut self, _packet: &[u8]) -> Vec<i16> {
            vec![0; super::FRAME]
        }
    }
}

pub use imp::{Decoder, Encoder};
