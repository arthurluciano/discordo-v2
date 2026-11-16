package pion

import (
	"encoding/binary"
	"strings"
	"time"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

// Simulcast layers by rid, best first. A track without simulcast has a single layer, "".
var layerOrder = []string{"h", "m", "l"}

// qualityLevel is the index into layerOrder each rtc_subscribe quality starts from ("auto" starts at the top).
var qualityLevel = map[string]int{"auto": 0, "high": 0, "medium": 1, "low": 2}

// pickLayer is the layer closest to level that the publisher sends: the next lower one if that one is
// missing, else the next higher one.
func pickLayer[T any](layers map[string]T, level int) string {
	if _, ok := layers[""]; ok {
		return ""
	}
	for i := level; i < len(layerOrder); i++ {
		if _, ok := layers[layerOrder[i]]; ok {
			return layerOrder[i]
		}
	}
	for i := level - 1; i >= 0; i-- {
		if _, ok := layers[layerOrder[i]]; ok {
			return layerOrder[i]
		}
	}
	for rid := range layers { // rids we do not know: any of them
		return rid
	}
	return ""
}

// autoQuality is the "auto" subscription: the top layer unless the subscriber reports loss.
// shortcut: it only reads receiver-report loss, not a bandwidth estimate (TWCC/REMB): more than ~10% steps one
// layer down, 10 reports in a row under ~2% (~10 s, browsers report about once a second) step one back up.
// Use the congestion controller's estimate if this oscillates or reacts too late.
type autoQuality struct {
	level int // into layerOrder
	clean int
}

// report takes a receiver report's fraction lost (out of 256) and tells whether the level changed.
func (a *autoQuality) report(fractionLost uint8) bool {
	switch {
	case fractionLost > 25:
		a.clean = 0
		if a.level < len(layerOrder)-1 {
			a.level++
			return true
		}
	case fractionLost < 5:
		a.clean++
		if a.clean >= 10 && a.level > 0 {
			a.level--
			a.clean = 0
			return true
		}
	default:
		a.clean = 0
	}
	return false
}

// rewriter maps the sequence numbers and timestamps of whichever layer is being forwarded onto one continuous
// stream: TrackLocalStaticRTP only rewrites SSRC and payload type, and each layer numbers its packets on its own.
type rewriter struct {
	seqOff  uint16
	tsOff   uint32
	lastSeq uint16
	lastTS  uint32
	lastAt  time.Time
	started bool
}

// switchTo makes the packet (seq, ts), the first of a new layer, follow the last one forwarded.
func (w *rewriter) switchTo(seq uint16, ts uint32, clockRate uint32, now time.Time) {
	if !w.started {
		w.seqOff, w.tsOff = 0, 0
		return
	}
	w.seqOff = w.lastSeq + 1 - seq
	// The new layer's first frame lands as far after the last one as it does in real time.
	elapsed := max(uint32(now.Sub(w.lastAt).Seconds()*float64(clockRate)), 1)
	w.tsOff = w.lastTS + elapsed - ts
}

func (w *rewriter) rewrite(seq uint16, ts uint32, now time.Time) (uint16, uint32) {
	seq, ts = seq+w.seqOff, ts+w.tsOff
	if !w.started || int16(seq-w.lastSeq) > 0 { // retransmissions and reordered packets do not move the stream back
		w.lastSeq, w.lastTS, w.lastAt, w.started = seq, ts, now, true
	}
	return seq, ts
}

// isKeyframe tells whether an RTP payload starts a keyframe, where a subscriber can switch layers.
// Codecs we cannot parse always say yes: switching right away beats never switching, and the PLI sent
// with the switch makes the decoder recover.
func isKeyframe(mimeType string, payload []byte) bool {
	switch strings.ToLower(mimeType) {
	case strings.ToLower(webrtc.MimeTypeH264):
		return h264Keyframe(payload)
	case strings.ToLower(webrtc.MimeTypeVP8):
		return vp8Keyframe(payload)
	case strings.ToLower(webrtc.MimeTypeVP9):
		var vp9 codecs.VP9Packet // the start of an intra (not inter-predicted) frame in the base spatial layer
		_, err := vp9.Unmarshal(payload)
		return err == nil && vp9.B && !vp9.P && vp9.SID == 0
	case strings.ToLower(webrtc.MimeTypeAV1):
		return len(payload) > 0 && payload[0]&0x08 != 0 // N: the first packet of a coded video sequence
	}
	return true
}

// h264Keyframe looks for an IDR slice (5) or a sequence parameter set (7), which precedes the IDR in browsers' streams.
func h264Keyframe(p []byte) bool {
	if len(p) < 1 {
		return false
	}
	isKey := func(nal byte) bool { nal &= 0x1F; return nal == 5 || nal == 7 }
	switch nal := p[0] & 0x1F; nal {
	case 24: // STAP-A: [header] then (2-byte size, NAL unit)...
		for i := 1; i+2 < len(p); {
			size := int(binary.BigEndian.Uint16(p[i:]))
			if isKey(p[i+2]) {
				return true
			}
			i += 2 + size
		}
		return false
	case 28: // FU-A: [indicator][header: S E R type]; only the fragment that starts the NAL unit counts
		return len(p) > 1 && p[1]&0x80 != 0 && isKey(p[1])
	default:
		return isKey(nal)
	}
}

// vp8Keyframe parses the VP8 payload descriptor (RFC 7741) down to the frame header's P bit (0: keyframe).
func vp8Keyframe(p []byte) bool {
	if len(p) < 1 || p[0]&0x10 == 0 || p[0]&0x07 != 0 {
		return false // not the start of partition 0
	}
	i := 1
	if p[0]&0x80 != 0 { // X: extensions follow
		if len(p) < 2 {
			return false
		}
		x := p[1]
		i = 2
		if x&0x80 != 0 { // I: picture ID, 7 or 15 bits
			if len(p) <= i {
				return false
			}
			if p[i]&0x80 != 0 {
				i++
			}
			i++
		}
		if x&0x40 != 0 { // L: TL0PICIDX
			i++
		}
		if x&0x30 != 0 { // T or K: TID/Y/KEYIDX
			i++
		}
	}
	return len(p) > i && p[i]&0x01 == 0
}
