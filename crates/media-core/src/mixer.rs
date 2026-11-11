//! Mixes the voices of everyone in the call into one output signal.

use std::collections::{HashMap, VecDeque};

use crate::codec::RATE;

/// A voice starts playing once this much is buffered, so ordinary network jitter does not cause gaps.
const PREBUFFER: usize = RATE as usize * 60 / 1000;
/// Beyond this the listener would lag behind the conversation; the oldest audio is dropped instead.
const MAX_BUFFERED: usize = RATE as usize * 200 / 1000;

#[derive(Default)]
struct Voice {
    samples: VecDeque<i16>,
    playing: bool,
}

/// shortcut: a fixed prebuffer and silence on underrun; replace with an adaptive jitter buffer and
/// Opus packet-loss concealment if calls on bad networks sound choppy.
#[derive(Default)]
pub struct Mixer {
    voices: HashMap<String, Voice>,
}

impl Mixer {
    /// Queues decoded mono audio from one source.
    pub fn push(&mut self, source: &str, pcm: &[i16]) {
        let voice = self.voices.entry(source.to_string()).or_default();
        voice.samples.extend(pcm);
        let excess = voice.samples.len().saturating_sub(MAX_BUFFERED);
        voice.samples.drain(..excess);
    }

    pub fn remove(&mut self, source: &str) {
        self.voices.remove(source);
    }

    /// Fills `out` with the sum of all voices (silence where there is none).
    pub fn mix(&mut self, out: &mut [i16]) {
        out.fill(0);
        for voice in self.voices.values_mut() {
            if !voice.playing && voice.samples.len() < PREBUFFER {
                continue;
            }
            let n = voice.samples.len().min(out.len());
            for (o, s) in out.iter_mut().zip(voice.samples.drain(..n)) {
                *o = o.saturating_add(s);
            }
            voice.playing = !voice.samples.is_empty(); // ran dry: prebuffer again before resuming
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn waits_for_the_prebuffer_then_sums_and_clips() {
        let mut mixer = Mixer::default();
        let mut out = [1i16; 4];

        mixer.push("a", &[100; PREBUFFER - 1]);
        mixer.mix(&mut out);
        assert_eq!(out, [0; 4], "below the prebuffer nothing plays");

        mixer.push("a", &[100; 5]);
        mixer.push("b", &[i16::MAX; PREBUFFER]);
        mixer.mix(&mut out);
        assert_eq!(out, [i16::MAX; 4], "voices add up without wrapping around");

        mixer.remove("b");
        mixer.mix(&mut out);
        assert_eq!(out, [100; 4], "a keeps playing once started");
    }

    #[test]
    fn drops_the_oldest_audio_when_too_far_behind() {
        let mut mixer = Mixer::default();
        mixer.push("a", &[1; MAX_BUFFERED]);
        mixer.push("a", &[2; 10]);
        let mut out = vec![0i16; MAX_BUFFERED];
        mixer.mix(&mut out);
        assert_eq!(out[0], 1);
        assert_eq!(out[MAX_BUFFERED - 10..], [2; 10]);
    }
}
