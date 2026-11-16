package pion

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestPickLayer(t *testing.T) {
	all := map[string]bool{"h": true, "m": true, "l": true}
	hl := map[string]bool{"h": true, "l": true}
	for _, c := range []struct {
		layers map[string]bool
		level  int
		want   string
	}{
		{all, 0, "h"}, {all, 1, "m"}, {all, 2, "l"},
		{hl, 1, "l"},                         // medium missing: the next lower one
		{map[string]bool{"h": true}, 2, "h"}, // nothing lower: the next higher one
		{map[string]bool{"": true}, 2, ""},   // no simulcast
		{map[string]bool{"q": true}, 0, "q"}, // rids we do not know
	} {
		if got := pickLayer(c.layers, c.level); got != c.want {
			t.Errorf("pickLayer(%v, %d) = %q, want %q", c.layers, c.level, got, c.want)
		}
	}
}

func TestAutoQuality(t *testing.T) {
	var a autoQuality
	if !a.report(60) || a.level != 1 {
		t.Fatalf("heavy loss should step down, level %d", a.level)
	}
	if !a.report(60) || a.report(60) || a.level != 2 {
		t.Fatalf("should stop at the lowest layer, level %d", a.level)
	}
	for i := range 9 {
		if a.report(0) {
			t.Fatalf("stepped up after only %d clean reports", i+1)
		}
	}
	if !a.report(0) || a.level != 1 {
		t.Fatalf("10 clean reports should step up, level %d", a.level)
	}
}

func TestRewriter(t *testing.T) {
	var w rewriter
	now := time.Now()
	w.switchTo(100, 5000, 90000, now)
	for i := range uint16(3) {
		seq, ts := w.rewrite(100+i, 5000, now)
		if seq != 100+i || ts != 5000 {
			t.Fatalf("first layer should pass through, got %d/%d", seq, ts)
		}
	}
	// A retransmission of an older packet keeps its place and does not move the stream back.
	if seq, _ := w.rewrite(101, 5000, now); seq != 101 {
		t.Fatalf("retransmission got seq %d", seq)
	}
	// Switch to a layer numbered somewhere else entirely (and wrapping around), 100 ms later.
	later := now.Add(100 * time.Millisecond)
	w.switchTo(65535, 4_000_000_000, 90000, later)
	seq, ts := w.rewrite(65535, 4_000_000_000, later)
	if seq != 103 || ts != 5000+9000 {
		t.Fatalf("after switch got %d/%d, want 103/14000", seq, ts)
	}
	if seq, ts := w.rewrite(0, 4_000_003_000, later); seq != 104 || ts != 17000 {
		t.Fatalf("next packet got %d/%d, want 104/17000", seq, ts)
	}
}

func TestIsKeyframe(t *testing.T) {
	h264 := webrtc.MimeTypeH264
	for _, c := range []struct {
		name    string
		mime    string
		payload []byte
		want    bool
	}{
		{"h264 idr", h264, []byte{0x65, 0}, true},
		{"h264 sps", h264, []byte{0x67, 0}, true},
		{"h264 non-idr slice", h264, []byte{0x41, 0}, false},
		{"h264 stap-a with sps", h264, []byte{0x78, 0, 2, 0x67, 0, 0, 2, 0x68, 0}, true},
		{"h264 stap-a without", h264, []byte{0x78, 0, 2, 0x41, 0, 0, 2, 0x41, 0}, false},
		{"h264 fu-a idr start", h264, []byte{0x7C, 0x85, 0}, true},
		{"h264 fu-a idr middle", h264, []byte{0x7C, 0x05, 0}, false},
		{"h264 empty", h264, nil, false},
		{"vp8 keyframe", webrtc.MimeTypeVP8, []byte{0x10, 0x00}, true},
		{"vp8 keyframe, 15-bit picture id", webrtc.MimeTypeVP8, []byte{0x90, 0x80, 0x81, 0x23, 0x00}, true},
		{"vp8 interframe", webrtc.MimeTypeVP8, []byte{0x10, 0x01}, false},
		{"vp8 not partition start", webrtc.MimeTypeVP8, []byte{0x00, 0x00}, false},
		{"vp9 keyframe", webrtc.MimeTypeVP9, []byte{0x08, 0x00}, true},
		{"vp9 keyframe, 15-bit picture id", webrtc.MimeTypeVP9, []byte{0x88, 0x80, 0x01, 0x00}, true},
		{"vp9 interframe", webrtc.MimeTypeVP9, []byte{0x48, 0x00}, false},
		{"vp9 not frame start", webrtc.MimeTypeVP9, []byte{0x00, 0x00}, false},
		{"av1 new sequence", webrtc.MimeTypeAV1, []byte{0x18, 0x00}, true},
		{"av1 other", webrtc.MimeTypeAV1, []byte{0x10, 0x00}, false},
		{"unknown codec", webrtc.MimeTypeH265, []byte{0}, true},
	} {
		if got := isKeyframe(c.mime, c.payload); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}
